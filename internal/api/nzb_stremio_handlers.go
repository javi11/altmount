package api

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/database"
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

// StremioStream represents a single stream entry in the Stremio addon format.
type StremioStream struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Name  string `json:"name"`
}

// StremioStreamsResponse is the response returned by the Stremio stream endpoint.
// The _queue_item_id and _queue_status fields are AltMount extensions that Stremio ignores.
type StremioStreamsResponse struct {
	Streams     []StremioStream `json:"streams"`
	QueueItemID int64           `json:"_queue_item_id"`
	QueueStatus string          `json:"_queue_status"`
}

// handleNzbStremioStreams handles POST /api/nzb/stremio-streams.
// Public endpoint — authenticated via the download_key form field (SHA256 of the user's API key).
// Accepts an NZB file, adds it to the import queue with high priority, and waits synchronously
// for processing to complete before returning Stremio-compatible stream URLs for all media files
// found in the NZB output.
func (s *Server) handleNzbStremioStreams(c *fiber.Ctx) error {
	ctx := c.Context()

	// --- Authenticate via download_key ---
	downloadKey := c.FormValue("download_key")
	if downloadKey == "" {
		return RespondUnauthorized(c, "download_key is required", "Provide the SHA256 hash of your API key")
	}

	if s.userRepo == nil {
		return RespondInternalError(c, "User repository not available", "")
	}

	users, err := s.userRepo.GetAllUsers(ctx)
	if err != nil {
		return RespondInternalError(c, "Failed to authenticate", err.Error())
	}

	authenticated := false
	for _, user := range users {
		if user.APIKey == nil || *user.APIKey == "" {
			continue
		}
		if hashAPIKey(*user.APIKey) == downloadKey {
			authenticated = true
			break
		}
	}

	if !authenticated {
		slog.WarnContext(ctx, "Stremio stream endpoint: authentication failed - invalid download_key")
		return RespondUnauthorized(c, "Invalid download_key", "")
	}

	// --- Validate uploaded NZB file ---
	file, err := c.FormFile("file")
	if err != nil {
		return RespondBadRequest(c, "No file provided", "A .nzb file must be uploaded")
	}

	if !strings.HasSuffix(strings.ToLower(file.Filename), ".nzb") {
		return RespondValidationError(c, "Invalid file type", "Only .nzb files are allowed")
	}

	const maxUploadSize = 100 * 1024 * 1024 // 100 MB
	if file.Size > maxUploadSize {
		return RespondValidationError(c, "File too large", "File size must be less than 100MB")
	}

	// --- Optional parameters ---
	baseURL := strings.TrimRight(c.FormValue("base_url"), "/")
	if baseURL == "" {
		baseURL = c.Protocol() + "://" + c.Hostname()
	}

	category := c.FormValue("category")

	timeoutSecs := 300
	if ts := c.FormValue("timeout"); ts != "" {
		if n, err := strconv.Atoi(ts); err == nil && n > 0 {
			timeoutSecs = n
		}
	}

	// --- Derive stable names before touching the filesystem ---
	uploadDir := filepath.Join(os.TempDir(), "altmount-uploads")
	safeFilename := filepath.Base(file.Filename)
	nzbName := strings.TrimSuffix(safeFilename, filepath.Ext(safeFilename))
	tempPath := filepath.Join(uploadDir, safeFilename)

	// --- Short-circuit: return cached streams if NZB was already processed ---
	completedStatus := database.QueueStatusCompleted
	existing, err := s.queueRepo.ListQueueItems(ctx, &completedStatus, safeFilename, "", 1, 0, "updated_at", "desc")
	if err == nil && len(existing) > 0 {
		if prev := existing[0]; prev.StoragePath != nil && *prev.StoragePath != "" {
			if streams, err := s.buildStremioStreams(prev, baseURL, downloadKey, nzbName); err == nil {
				slog.InfoContext(ctx, "Returning cached Stremio streams for already-processed NZB",
					"nzb_name", nzbName, "queue_id", prev.ID)
				return c.JSON(StremioStreamsResponse{
					Streams:     streams,
					QueueItemID: prev.ID,
					QueueStatus: string(prev.Status),
				})
			}
		}
	}

	// --- Short-circuit: join existing active queue item instead of re-adding ---
	if inQueue, _ := s.queueRepo.IsFileInQueue(ctx, tempPath); inQueue {
		activeItems, err := s.queueRepo.ListQueueItems(ctx, nil, safeFilename, "", 1, 0, "updated_at", "desc")
		if err == nil && len(activeItems) > 0 {
			return s.pollAndRespond(c, activeItems[0].ID, baseURL, downloadKey, nzbName, timeoutSecs)
		}
	}

	// --- Save NZB to temp directory ---
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return RespondInternalError(c, "Failed to create upload directory", err.Error())
	}

	if err := c.SaveFile(file, tempPath); err != nil {
		return RespondInternalError(c, "Failed to save uploaded file", err.Error())
	}

	// --- Add NZB to queue with high priority ---
	if s.importerService == nil {
		os.Remove(tempPath)
		return RespondServiceUnavailable(c, "Importer service not available", "")
	}

	var categoryPtr *string
	if category != "" {
		categoryPtr = &category
	}

	var basePath *string
	if s.configManager != nil {
		if completeDir := s.configManager.GetConfig().SABnzbd.CompleteDir; completeDir != "" {
			basePath = &completeDir
		}
	}

	priority := database.QueuePriorityHigh
	item, err := s.importerService.AddToQueue(ctx, tempPath, basePath, categoryPtr, &priority)
	if err != nil {
		os.Remove(tempPath)
		return RespondInternalError(c, "Failed to add NZB to queue", err.Error())
	}

	slog.InfoContext(ctx, "NZB added to queue for Stremio stream processing",
		"queue_id", item.ID,
		"nzb_path", tempPath,
		"timeout_secs", timeoutSecs)

	return s.pollAndRespond(c, item.ID, baseURL, downloadKey, nzbName, timeoutSecs)
}

// pollAndRespond polls the queue item until it completes, fails, or times out,
// then builds and returns the Stremio stream response.
func (s *Server) pollAndRespond(c *fiber.Ctx, itemID int64, baseURL, downloadKey, nzbName string, timeoutSecs int) error {
	ctx := c.Context()
	deadline := time.Now().Add(time.Duration(timeoutSecs) * time.Second)

	for time.Now().Before(deadline) {
		current, err := s.queueRepo.GetQueueItem(ctx, itemID)
		if err != nil {
			return RespondInternalError(c, "Failed to check queue status", err.Error())
		}
		if current == nil {
			return RespondInternalError(c, "Queue item not found", fmt.Sprintf("item ID %d", itemID))
		}

		switch current.Status {
		case database.QueueStatusCompleted:
			streams, err := s.buildStremioStreams(current, baseURL, downloadKey, nzbName)
			if err != nil {
				return RespondInternalError(c, "Failed to list output media files", err.Error())
			}
			return c.JSON(StremioStreamsResponse{
				Streams:     streams,
				QueueItemID: current.ID,
				QueueStatus: string(current.Status),
			})

		case database.QueueStatusFailed:
			errMsg := ""
			if current.ErrorMessage != nil {
				errMsg = *current.ErrorMessage
			}
			return RespondInternalError(c, "NZB processing failed", errMsg)
		}

		// Still pending or processing — wait before polling again.
		time.Sleep(2 * time.Second)
	}

	return RespondError(c, fiber.StatusRequestTimeout, "TIMEOUT",
		"NZB processing timed out",
		fmt.Sprintf("Processing did not complete within %d seconds (queue_item_id: %d)", timeoutSecs, itemID))
}

// buildStremioStreams resolves the virtual paths from a completed queue item and
// returns Stremio stream objects for all media files in the NZB output.
func (s *Server) buildStremioStreams(item *database.ImportQueueItem, baseURL, downloadKey, nzbName string) ([]StremioStream, error) {
	if item.StoragePath == nil || *item.StoragePath == "" {
		return nil, fmt.Errorf("completed queue item %d has no storage path", item.ID)
	}

	storagePath := filepath.ToSlash(*item.StoragePath)

	// If the storage path already points to a media file, return it directly.
	if isMediaExtension(filepath.Ext(storagePath)) {
		return []StremioStream{stremioStreamFromPath(storagePath, baseURL, downloadKey, nzbName)}, nil
	}

	// Otherwise treat it as a virtual directory and list its media files.
	if s.metadataService == nil {
		return nil, fmt.Errorf("metadata service not available")
	}

	files, err := s.metadataService.ListDirectory(storagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory %q: %w", storagePath, err)
	}

	var streams []StremioStream
	for _, name := range files {
		if !isMediaExtension(filepath.Ext(name)) {
			continue
		}
		virtualPath := filepath.ToSlash(filepath.Join(storagePath, name))
		streams = append(streams, stremioStreamFromPath(virtualPath, baseURL, downloadKey, nzbName))
	}

	return streams, nil
}

// stremioStreamFromPath creates a StremioStream for a given virtual file path.
func stremioStreamFromPath(virtualPath, baseURL, downloadKey, nzbName string) StremioStream {
	streamURL := baseURL + "/api/files/stream?path=" +
		url.QueryEscape(virtualPath) + "&download_key=" + url.QueryEscape(downloadKey)
	return StremioStream{
		URL:   streamURL,
		Title: filepath.Base(virtualPath),
		Name:  nzbName,
	}
}

// isMediaExtension reports whether ext is a common video/media file extension.
func isMediaExtension(ext string) bool {
	return mediaExtensions[strings.ToLower(ext)]
}
