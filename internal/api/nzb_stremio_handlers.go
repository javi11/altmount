package api

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/importer/utils/nzbtrim"
)

// mediaExtensions lists common video/media file extensions for Stremio stream filtering.
var mediaExtensions = map[string]bool{
	".mkv":  true,
	".mp4":  true,
	".avi":  true,
	".ts":   true,
	".m2ts": true,
	".mov":  true,
	".wmv":  true,
	".flv":  true,
	".m4v":  true,
	".mpeg": true,
	".mpg":  true,
	".vob":  true,
	".webm": true,
	".ogv":  true,
	".iso":  true,
}

var (
	stremioSeasonEpisodePattern = regexp.MustCompile(`(?i)s0*(\d{1,2})((?:[ ._-]*e0*\d{1,3})+)`)
	stremioEpisodeOnlyPattern   = regexp.MustCompile(`(?i)e0*(\d{1,3})`)
	stremioXEpisodePattern      = regexp.MustCompile(`(?i)(?:^|[^0-9])0*(\d{1,2})x0*(\d{1,3})(?:[^0-9]|$)`)
)

type stremioEpisodeSelector struct {
	Season  int
	Episode int
}

// StremioStream represents a single stream entry in the Stremio addon format.
type StremioStream struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Name  string `json:"name"`
}

// StremioStreamsResponse is the response returned by the Stremio stream endpoint.
// The _queue_item_id, _queue_status, and _cached fields are AltMount extensions that Stremio ignores.
type StremioStreamsResponse struct {
	Streams     []StremioStream `json:"streams"`
	QueueItemID int64           `json:"_queue_item_id"`
	QueueStatus string          `json:"_queue_status"`
	// Cached is true when streams were served from an already-completed queue item
	// without re-processing. Callers such as AIOStreams can use this to show an
	// "instant" indicator to the user.
	Cached bool `json:"_cached"`
}

// handleNzbStreams handles POST /api/nzb/streams.
// Public endpoint — authenticated via the download_key form field (SHA256 of the user's API key).
// Accepts an NZB file, adds it to the import queue with high priority, and waits synchronously
// for processing to complete before returning Stremio-compatible stream URLs for all media files
// found in the NZB output.
//
//	@Summary		Get Stremio streams for NZB file
//	@Description	Accepts an NZB file (upload or URL), queues it with high priority, and returns Stremio-compatible stream URLs as soon as the file is accessible via VFS. Auth: download_key form field (SHA256 of API key) or X-Api-Key header (raw API key). Returns _cached=true when served from an already-completed item.
//	@Tags			Stremio
//	@Accept			multipart/form-data
//	@Produce		json
//	@Param			file			formData	file	false	"NZB file to process (mutually exclusive with nzb_url)"
//	@Param			nzb_url			formData	string	false	"URL to download the NZB from (mutually exclusive with file)"
//	@Param			download_key	formData	string	false	"SHA256 hash of the user's API key (alternative: X-Api-Key header)"
//	@Param			X-Api-Key		header		string	false	"Raw API key (alternative to download_key form field)"
//	@Param			category		formData	string	false	"Queue category (default: stremio)"
//	@Param			timeout			formData	int		false	"Processing timeout in seconds (default: 300)"
//	@Param			season			formData	int		false	"Season number for selecting one episode from a season pack"
//	@Param			episode			formData	int		false	"Episode number for selecting one episode from a season pack"
//	@Success		200	{object}	StremioStreamsResponse
//	@Failure		400	{object}	APIResponse
//	@Failure		401	{object}	APIResponse
//	@Failure		503	{object}	APIResponse
//	@Router			/nzb/streams [post]
func (s *Server) handleNzbStreams(c *fiber.Ctx) error {
	ctx := c.Context()

	// --- Gate on Stremio enabled flag ---
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration not available", "")
	}
	cfg := s.configManager.GetConfig()
	if !isStremioEnabled(cfg) {
		return RespondNotFound(c, "Stremio endpoint", "Stremio integration is disabled in configuration")
	}

	// --- Authenticate via download_key form field, X-Api-Key header, or query param ---
	// download_key: SHA256 hash of the API key (used by the Stremio addon play path)
	// X-Api-Key:    raw API key (used by AIOStreams and other integrations)
	downloadKey := c.FormValue("download_key")
	if downloadKey == "" {
		downloadKey = c.Query("download_key")
	}
	if downloadKey == "" {
		// Accept raw API key via X-Api-Key header (AIOStreams sends altmountApiKey here).
		// Hash it so validateDownloadKey can compare against the stored hash.
		if rawKey := c.Get("X-Api-Key"); rawKey != "" {
			if s.validateAPIKey(c, rawKey) {
				downloadKey = auth.HashAPIKey(rawKey)
			} else {
				slog.WarnContext(ctx, "Stremio stream endpoint: authentication failed - invalid X-Api-Key")
				return RespondUnauthorized(c, "Invalid X-Api-Key", "")
			}
		}
	}
	if downloadKey == "" {
		return RespondUnauthorized(c, "Authentication required", "Provide download_key (SHA256 of API key) or X-Api-Key header")
	}
	if !s.validateDownloadKey(ctx, downloadKey) {
		slog.WarnContext(ctx, "Stremio stream endpoint: authentication failed - invalid download_key")
		return RespondUnauthorized(c, "Invalid download_key", "")
	}

	// --- Accept NZB as file upload or by URL ---
	// nzb_url allows callers (e.g. AIOStreams) to pass the NZB download URL directly
	// instead of uploading the file bytes, avoiding an extra round-trip.
	nzbURL := c.FormValue("nzb_url")

	var nzbFilename string
	var nzbData []byte

	if nzbURL != "" {
		// Download NZB from the provided URL
		const maxNzbFetchSize = 100 * 1024 * 1024 // 100 MB
		resp, err := http.Get(nzbURL)             //nolint:gosec // URL is provided by an authenticated caller
		if err != nil {
			return RespondBadRequest(c, "Failed to fetch NZB from URL", err.Error())
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return RespondBadRequest(c, "Failed to fetch NZB from URL", fmt.Sprintf("HTTP %d", resp.StatusCode))
		}
		nzbData, err = io.ReadAll(io.LimitReader(resp.Body, maxNzbFetchSize))
		if err != nil {
			return RespondBadRequest(c, "Failed to read NZB from URL", err.Error())
		}
		// Derive filename from URL path
		nzbFilename = filepath.Base(nzbURL)
		if idx := strings.Index(nzbFilename, "?"); idx >= 0 {
			nzbFilename = nzbFilename[:idx]
		}
		if !nzbtrim.HasNzbExtension(nzbFilename) {
			nzbFilename = nzbFilename + ".nzb"
		}
	} else {
		// Require file upload
		file, err := c.FormFile("file")
		if err != nil {
			return RespondBadRequest(c, "No file provided", "Upload a .nzb file or provide nzb_url")
		}
		if !nzbtrim.HasNzbExtension(file.Filename) {
			return RespondValidationError(c, "Invalid file type", "Only .nzb or .nzb.gz files are allowed")
		}
		const maxUploadSize = 100 * 1024 * 1024 // 100 MB
		if file.Size > maxUploadSize {
			return RespondValidationError(c, "File too large", "File size must be less than 100MB")
		}
		nzbFilename = file.Filename
		// Read file bytes for saving below
		f, err := file.Open()
		if err != nil {
			return RespondInternalError(c, "Failed to open uploaded file", err.Error())
		}
		defer f.Close()
		nzbData, err = io.ReadAll(f)
		if err != nil {
			return RespondInternalError(c, "Failed to read uploaded file", err.Error())
		}
	}

	// --- Resolve base URL ---
	baseURL := resolveBaseURL(c, cfg.Stremio.BaseURL)
	selector := stremioEpisodeSelectorFromRequest(c)

	category := c.FormValue("category")

	timeoutSecs := 300
	if ts := c.FormValue("timeout"); ts != "" {
		if n, err := strconv.Atoi(ts); err == nil && n > 0 {
			timeoutSecs = n
		}
	}

	// --- Derive stable names before touching the filesystem ---
	uploadDir := filepath.Join(os.TempDir(), "altmount-uploads")
	safeFilename := filepath.Base(nzbFilename)
	nzbName := nzbtrim.TrimNzbExtension(safeFilename)
	tempPath := filepath.Join(uploadDir, safeFilename)

	// --- Short-circuit: return cached streams if NZB was already processed ---
	// Respect the configurable TTL: 0 means cache forever, >0 means re-process after N hours.
	ttlHours := cfg.Stremio.NzbTTLHours

	completedStatus := database.QueueStatusCompleted
	existing, err := s.queueRepo.ListQueueItems(ctx, &completedStatus, safeFilename, "", 1, 0, "updated_at", "desc")
	if err == nil && len(existing) > 0 {
		prev := existing[0]
		cacheValid := prev.StoragePath != nil && *prev.StoragePath != ""
		if cacheValid && ttlHours > 0 && prev.CompletedAt != nil {
			cacheValid = time.Since(*prev.CompletedAt) < time.Duration(ttlHours)*time.Hour
		}
		if cacheValid {
			if streams, err := s.buildStremioStreams(prev, baseURL, downloadKey, nzbName, selector); err == nil {
				slog.InfoContext(ctx, "Returning cached Stremio streams for already-processed NZB",
					"nzb_name", nzbName, "queue_id", prev.ID)
				return c.JSON(StremioStreamsResponse{
					Streams:     streams,
					QueueItemID: prev.ID,
					QueueStatus: string(prev.Status),
					Cached:      true,
				})
			}
		}
	}

	// --- Short-circuit: join existing active queue item instead of re-adding ---
	if inQueue, _ := s.queueRepo.IsFileInQueue(ctx, tempPath); inQueue {
		activeItems, err := s.queueRepo.ListQueueItems(ctx, nil, safeFilename, "", 1, 0, "updated_at", "desc")
		if err == nil && len(activeItems) > 0 {
			return s.waitAndRespond(c, activeItems[0].ID, baseURL, downloadKey, nzbName, selector, timeoutSecs)
		}
	}

	// --- Save NZB to temp directory ---
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return RespondInternalError(c, "Failed to create upload directory", err.Error())
	}

	if err := os.WriteFile(tempPath, nzbData, 0644); err != nil {
		return RespondInternalError(c, "Failed to save NZB file", err.Error())
	}

	// --- Add NZB to queue with high priority ---
	if s.importerService == nil {
		os.Remove(tempPath)
		return RespondServiceUnavailable(c, "Importer service not available", "")
	}

	if category == "" {
		category = "stremio"
	}

	var basePath *string
	if completeDir := cfg.SABnzbd.CompleteDir; completeDir != "" {
		basePath = &completeDir
	}

	priority := database.QueuePriorityHigh
	item, err := s.importerService.AddToQueue(ctx, tempPath, basePath, &category, &priority, nil, nil)
	if err != nil {
		os.Remove(tempPath)
		return RespondInternalError(c, "Failed to add NZB to queue", err.Error())
	}

	slog.InfoContext(ctx, "NZB added to queue for Stremio stream processing",
		"queue_id", item.ID,
		"nzb_path", tempPath,
		"timeout_secs", timeoutSecs)

	return s.waitAndRespond(c, item.ID, baseURL, downloadKey, nzbName, selector, timeoutSecs)
}

// waitAndRespond subscribes to the progress broadcaster and waits for the queue item to
// reach a terminal state (completed or failed), then returns the appropriate Stremio response.
// This avoids polling by using an event-driven approach via the ProgressBroadcaster.
func (s *Server) waitAndRespond(c *fiber.Ctx, itemID int64, baseURL, downloadKey, nzbName string, selector *stremioEpisodeSelector, timeoutSecs int) error {
	ctx := c.Context()

	// Subscribe before the status check to eliminate the race between AddToQueue and the event.
	subID, ch := s.progressBroadcaster.Subscribe()
	defer s.progressBroadcaster.Unsubscribe(subID)

	// Single upfront check — the item may have already reached a terminal state.
	current, err := s.queueRepo.GetQueueItem(ctx, itemID)
	if err != nil {
		return RespondInternalError(c, "Failed to check queue status", err.Error())
	}
	if current == nil {
		return RespondInternalError(c, "Queue item not found", fmt.Sprintf("item ID %d", itemID))
	}

	switch current.Status {
	case database.QueueStatusCompleted:
		streams, err := s.buildStremioStreams(current, baseURL, downloadKey, nzbName, selector)
		if err != nil {
			return RespondInternalError(c, "Failed to list output media files", err.Error())
		}
		return c.JSON(StremioStreamsResponse{
			Streams:     streams,
			QueueItemID: current.ID,
			QueueStatus: string(current.Status),
			Cached:      true,
		})
	case database.QueueStatusFailed:
		errMsg := ""
		if current.ErrorMessage != nil {
			errMsg = *current.ErrorMessage
		}
		return RespondInternalError(c, "NZB processing failed", errMsg)
	default:
		// If the item is already processing and has a storage path, the streamable
		// event fired before we subscribed — return the streams immediately.
		if current.StoragePath != nil && *current.StoragePath != "" {
			if streams, err := s.buildStremioStreams(current, baseURL, downloadKey, nzbName, selector); err == nil && len(streams) > 0 {
				return c.JSON(StremioStreamsResponse{
					Streams:     streams,
					QueueItemID: current.ID,
					QueueStatus: "streamable",
				})
			}
		}
	}

	// Wait for a streamable or completion event from the broadcaster.
	timer := time.NewTimer(time.Duration(timeoutSecs) * time.Second)
	defer timer.Stop()

	for {
		select {
		case update, ok := <-ch:
			if !ok {
				return RespondInternalError(c, "Progress broadcaster closed unexpectedly", "")
			}
			if update.QueueID != int(itemID) {
				continue
			}
			switch update.Status {
			case "streamable":
				// Return streams as soon as the file is accessible in the VFS — before
				// post-processing (symlinks, STRM, health scheduling) completes.
				if update.StoragePath != "" {
					fakeItem := &database.ImportQueueItem{ID: itemID, StoragePath: &update.StoragePath}
					if streams, err := s.buildStremioStreams(fakeItem, baseURL, downloadKey, nzbName, selector); err == nil && len(streams) > 0 {
						return c.JSON(StremioStreamsResponse{
							Streams:     streams,
							QueueItemID: itemID,
							QueueStatus: "streamable",
						})
					}
				}
				// StoragePath empty or no media files yet — fall through to wait for completed.
			case "completed":
				item, err := s.queueRepo.GetQueueItem(ctx, itemID)
				if err != nil {
					return RespondInternalError(c, "Failed to fetch completed item", err.Error())
				}
				streams, err := s.buildStremioStreams(item, baseURL, downloadKey, nzbName, selector)
				if err != nil {
					return RespondInternalError(c, "Failed to list output media files", err.Error())
				}
				return c.JSON(StremioStreamsResponse{
					Streams:     streams,
					QueueItemID: item.ID,
					QueueStatus: string(item.Status),
				})
			case "failed":
				item, _ := s.queueRepo.GetQueueItem(ctx, itemID)
				errMsg := "Processing failed"
				if item != nil && item.ErrorMessage != nil {
					errMsg = *item.ErrorMessage
				}
				return RespondInternalError(c, errMsg, "")
			}
		case <-timer.C:
			return RespondError(c, fiber.StatusRequestTimeout, "TIMEOUT",
				"NZB processing timed out",
				fmt.Sprintf("Processing did not complete within %d seconds (queue_item_id: %d)", timeoutSecs, itemID))
		}
	}
}

// buildStremioStreams resolves the virtual paths from a completed queue item and
// returns Stremio stream objects for all media files in the NZB output.
func (s *Server) buildStremioStreams(item *database.ImportQueueItem, baseURL, downloadKey, nzbName string, selector *stremioEpisodeSelector) ([]StremioStream, error) {
	if item.StoragePath == nil || *item.StoragePath == "" {
		return nil, fmt.Errorf("completed queue item %d has no storage path", item.ID)
	}

	storagePath := filepath.ToSlash(*item.StoragePath)

	// If the storage path already points to a media file, return it directly.
	if isMediaExtension(filepath.Ext(storagePath)) {
		if selector != nil && !selector.matches(filepath.Base(storagePath)) {
			return []StremioStream{}, nil
		}
		return []StremioStream{stremioStreamFromPath(storagePath, baseURL, downloadKey)}, nil
	}

	// Otherwise treat it as a virtual directory and list its media files.
	if s.metadataService == nil {
		return nil, fmt.Errorf("metadata service not available")
	}

	files, err := s.listStremioMediaFiles(storagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory %q: %w", storagePath, err)
	}

	streams := make([]StremioStream, 0, len(files))
	for _, name := range files {
		if selector != nil && !selector.matches(name) {
			continue
		}
		virtualPath := filepath.ToSlash(filepath.Join(storagePath, filepath.FromSlash(name)))
		streams = append(streams, stremioStreamFromPath(virtualPath, baseURL, downloadKey))
	}

	return streams, nil
}

func (s *Server) listStremioMediaFiles(storagePath string) ([]string, error) {
	dirs, files, err := s.metadataService.ListDirectoryAll(storagePath)
	if err != nil {
		return nil, err
	}

	mediaFiles := make([]string, 0, len(files))
	for _, name := range files {
		if isMediaExtension(filepath.Ext(name)) {
			mediaFiles = append(mediaFiles, name)
		}
	}

	for _, dir := range dirs {
		if dir == nil {
			continue
		}
		children, err := s.listStremioMediaFiles(filepath.ToSlash(filepath.Join(storagePath, dir.Name())))
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			mediaFiles = append(mediaFiles, filepath.ToSlash(filepath.Join(dir.Name(), filepath.FromSlash(child))))
		}
	}

	return mediaFiles, nil
}

// stremioStreamFromPath creates a StremioStream for a given virtual file path.
func stremioStreamFromPath(virtualPath, baseURL, downloadKey string) StremioStream {
	streamURL := baseURL + "/api/files/stream?path=" +
		url.QueryEscape(virtualPath) + "&download_key=" + url.QueryEscape(downloadKey)
	filename := filepath.Base(virtualPath)
	return StremioStream{
		URL:   streamURL,
		Title: filename,
		Name:  filename,
	}
}

// isMediaExtension reports whether ext is a common video/media file extension.
func isMediaExtension(ext string) bool {
	return mediaExtensions[strings.ToLower(ext)]
}

func stremioEpisodeSelectorFromRequest(c *fiber.Ctx) *stremioEpisodeSelector {
	season, okSeason := positiveIntFormOrQuery(c, "season")
	episode, okEpisode := positiveIntFormOrQuery(c, "episode")
	if !okSeason || !okEpisode {
		return nil
	}
	return &stremioEpisodeSelector{Season: season, Episode: episode}
}

func positiveIntFormOrQuery(c *fiber.Ctx, key string) (int, bool) {
	value := c.FormValue(key)
	if value == "" {
		value = c.Query(key)
	}
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
}

func (s *stremioEpisodeSelector) matches(filename string) bool {
	if s == nil || s.Season <= 0 || s.Episode <= 0 {
		return true
	}

	for _, match := range stremioSeasonEpisodePattern.FindAllStringSubmatch(filename, -1) {
		season, err := strconv.Atoi(match[1])
		if err != nil || season != s.Season {
			continue
		}
		for _, episodeMatch := range stremioEpisodeOnlyPattern.FindAllStringSubmatch(match[2], -1) {
			episode, err := strconv.Atoi(episodeMatch[1])
			if err == nil && episode == s.Episode {
				return true
			}
		}
	}

	for _, match := range stremioXEpisodePattern.FindAllStringSubmatch(filename, -1) {
		season, seasonErr := strconv.Atoi(match[1])
		episode, episodeErr := strconv.Atoi(match[2])
		if seasonErr == nil && episodeErr == nil && season == s.Season && episode == s.Episode {
			return true
		}
	}

	return false
}
