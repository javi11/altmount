package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/prowlarr"
)

// stremioManifest is the Stremio addon manifest response.
type stremioManifest struct {
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Resources   []string `json:"resources"`
	Types       []string `json:"types"`
	Catalogs    []any    `json:"catalogs"`
	IDPrefixes  []string `json:"idPrefixes"`
}

// emptyStreamsResponse returns the Stremio-protocol empty streams JSON.
func emptyStreamsResponse(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"streams": []any{}})
}

// resolveBaseURL returns the public base URL for building stream links.
// Uses the configured base_url if set, otherwise auto-detects from the request.
func resolveBaseURL(c *fiber.Ctx, configuredURL string) string {
	baseURL := strings.TrimRight(configuredURL, "/")
	if baseURL == "" {
		baseURL = c.Protocol() + "://" + c.Hostname()
	}
	return baseURL
}

// isStremioEnabled reports whether the Stremio integration is active.
func isStremioEnabled(cfg *config.Config) bool {
	return cfg.Stremio.Enabled != nil && *cfg.Stremio.Enabled
}

// isProwlarrEnabled reports whether the Prowlarr search is active.
func isProwlarrEnabled(cfg *config.Config) bool {
	return cfg.Stremio.Prowlarr.Enabled != nil && *cfg.Stremio.Prowlarr.Enabled
}

// handleStremioManifest handles GET /stremio/:key/manifest.json
// Returns the Stremio addon manifest for addon installation.
//
//	@Summary		Stremio addon manifest
//	@Description	Returns the Stremio addon manifest JSON for installation. The key authenticates the addon.
//	@Tags			Stremio
//	@Produce		json
//	@Param			key	path		string	true	"Download key (SHA256 of API key)"
//	@Success		200	{object}	stremioManifest
//	@Failure		401	{object}	APIResponse
//	@Router			/stremio/{key}/manifest.json [get]
func (s *Server) handleStremioManifest(c *fiber.Ctx) error {
	ctx := c.Context()

	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration not available", "")
	}
	cfg := s.configManager.GetConfig()
	if !isStremioEnabled(cfg) {
		return RespondNotFound(c, "Stremio endpoint", "Stremio integration is disabled")
	}

	key := c.Params("key")
	if !s.validateDownloadKey(ctx, key) {
		return RespondUnauthorized(c, "Invalid key", "")
	}

	slog.InfoContext(ctx, "Stremio addon manifest requested")

	return c.JSON(stremioManifest{
		ID:          "community.altmount",
		Version:     "1.0.0",
		Name:        "AltMount Usenet",
		Description: "Stream from Usenet via Prowlarr",
		Resources:   []string{"stream"},
		Types:       []string{"movie", "series"},
		Catalogs:    []any{},
		IDPrefixes:  []string{"tt"},
	})
}

// handleStremioAddonStream handles GET /stremio/:key/stream/:type/:id.json
// Searches Prowlarr and returns play-URL options -- no NZB download or queuing at this stage.
//
//	@Summary		Stremio stream handler
//	@Description	Searches Prowlarr for matching NZBs and returns Stremio-compatible stream URL options.
//	@Tags			Stremio
//	@Produce		json
//	@Param			key		path		string	true	"Download key"
//	@Param			type	path		string	true	"Content type (movie or series)"
//	@Param			id		path		string	true	"Stremio content ID (e.g. tt1234567)"
//	@Success		200		{object}	APIResponse
//	@Failure		401		{object}	APIResponse
//	@Router			/stremio/{key}/stream/{type}/{id}.json [get]
func (s *Server) handleStremioAddonStream(c *fiber.Ctx) error {
	ctx := c.Context()

	if s.configManager == nil {
		return emptyStreamsResponse(c)
	}
	cfg := s.configManager.GetConfig()
	if !isStremioEnabled(cfg) || !isProwlarrEnabled(cfg) {
		return emptyStreamsResponse(c)
	}

	key := c.Params("key")
	if !s.validateDownloadKey(ctx, key) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid key"})
	}

	streamType := c.Params("type")
	rawID, _ := url.PathUnescape(c.Params("id"))

	// Parse Stremio ID: tt1234567 (movie) or tt1234567:season:episode (series)
	var season, episode int
	parts := strings.SplitN(rawID, ":", 3)
	imdbID := parts[0]
	if len(parts) >= 2 {
		season, _ = strconv.Atoi(parts[1])
	}
	if len(parts) >= 3 {
		episode, _ = strconv.Atoi(parts[2])
	}

	if !strings.HasPrefix(imdbID, "tt") {
		return emptyStreamsResponse(c)
	}

	// Map Stremio type to Prowlarr search type
	prowlarrType := "search"
	switch streamType {
	case "movie":
		prowlarrType = "movie"
	case "series":
		prowlarrType = "tvsearch"
	}

	slog.InfoContext(ctx, "Stremio addon stream request",
		"type", streamType, "id", rawID, "imdb_id", imdbID)

	baseURL := resolveBaseURL(c, cfg.Stremio.BaseURL)
	prowlarrCfg := cfg.Stremio.Prowlarr

	// Search Prowlarr -- return play-URL options immediately, no download yet
	client := prowlarr.NewClient(prowlarrCfg.Host, prowlarrCfg.APIKey)
	var (
		results []prowlarr.NZBResult
		err     error
		tvdbID  string
	)

	// For series, prefer TvdbId queries when possible and fall back to ImdbId.
	if streamType == "series" {
		tvdbID, err = resolveTVDBFromIMDb(ctx, imdbID)
		if err != nil {
			slog.WarnContext(ctx, "Failed to resolve TVDB ID from IMDb ID",
				"error", err, "imdb_id", imdbID)
			tvdbID = ""
		}

		if tvdbID != "" {
			slog.InfoContext(ctx, "Searching Prowlarr using TVDB ID for series",
				"imdb_id", imdbID, "tvdb_id", tvdbID, "season", season, "episode", episode)
			results, err = client.SearchByTVDB(ctx, tvdbID, prowlarrType, prowlarrCfg.Categories, season, episode)
			if err != nil {
				slog.WarnContext(ctx, "Prowlarr TVDB search failed; falling back to IMDb search",
					"error", err, "imdb_id", imdbID, "tvdb_id", tvdbID)
				results = nil
			}
		}
	}

	if len(results) == 0 {
		results, err = client.Search(ctx, imdbID, prowlarrType, prowlarrCfg.Categories, season, episode)
		if err != nil {
			slog.WarnContext(ctx, "Prowlarr search failed", "error", err, "imdb_id", imdbID, "tvdb_id", tvdbID)
			return emptyStreamsResponse(c)
		}
	}

	if len(results) == 0 {
		slog.InfoContext(ctx, "No Prowlarr results found", "imdb_id", imdbID, "tvdb_id", tvdbID)
		return emptyStreamsResponse(c)
	}

	// Apply language and quality filters
	filtered := results[:0]
	for _, r := range results {
		if !prowlarr.MatchesLanguage(r.Title, prowlarrCfg.Languages) {
			continue
		}
		if !prowlarr.MatchesQuality(r.Title, prowlarrCfg.Qualities) {
			continue
		}
		filtered = append(filtered, r)
	}
	results = filtered

	var streams []fiber.Map
	for _, r := range results {
		safeTitle := sanitizeFilename(r.Title)
		if safeTitle == "" {
			safeTitle = imdbID
		}
		playURL := baseURL + "/stremio/" + key + "/play" +
			"?url=" + url.QueryEscape(r.DownloadURL) +
			"&title=" + url.QueryEscape(safeTitle)

		sizeGB := float64(r.Size) / 1e9
		indexerLabel := r.Indexer
		if indexerLabel == "" {
			indexerLabel = "Unknown"
		}

		meta := prowlarr.InferReleaseMeta(r.Title)

		// Badge: "AltMount 🇪🇸 4K"
		badge := "AltMount"
		if meta.FlagEmoji != "" {
			badge += " " + meta.FlagEmoji
		}
		if meta.QualityLabel != "" {
			badge += " " + meta.QualityLabel
		}

		// Content info: "La película (2024) [2160p][Esp]"
		contentTitle := meta.ParsedTitle
		if contentTitle == "" {
			contentTitle = r.Title
		}
		if meta.Year > 0 {
			contentTitle += fmt.Sprintf(" (%d)", meta.Year)
		}
		if meta.Resolution != "" {
			contentTitle += " [" + meta.Resolution + "]"
		}
		if meta.LangCode != "" {
			contentTitle += "[" + meta.LangCode + "]"
		}

		streamName := badge
		if contentTitle != "" {
			streamName += " - " + contentTitle
		}

		metaLine := fmt.Sprintf("💾 %.2f GB 🌐 %s", sizeGB, indexerLabel)
		streams = append(streams, fiber.Map{
			"name":  streamName,
			"title": fmt.Sprintf("%s\n%s", r.Title, metaLine),
			"url":   playURL,
		})
	}

	return c.JSON(fiber.Map{"streams": streams})
}

// handleStremioAddonPlay handles GET /stremio/:key/play
// Downloads the NZB from Prowlarr, queues it with high priority, waits for completion,
// then 302-redirects to the first media stream URL.
//
//	@Summary		Play Stremio NZB stream
//	@Description	Downloads the NZB from Prowlarr by URL, queues it with high priority, waits for download completion, then redirects (302) to the first media stream URL.
//	@Tags			Stremio
//	@Produce		json
//	@Param			key		path	string	true	"Download key (SHA256 of API key)"
//	@Param			url		query	string	true	"Prowlarr NZB download URL"
//	@Param			title	query	string	false	"Safe filename title for the NZB"
//	@Success		302	{string}	string	"Redirects to media stream URL"
//	@Failure		400	{object}	APIResponse
//	@Failure		401	{object}	APIResponse
//	@Failure		503	{object}	APIResponse
//	@Router			/stremio/{key}/play [get]
func (s *Server) handleStremioAddonPlay(c *fiber.Ctx) error {
	ctx := c.Context()

	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration not available", "")
	}
	cfg := s.configManager.GetConfig()
	if !isStremioEnabled(cfg) {
		return RespondNotFound(c, "Stremio endpoint", "Stremio integration is disabled")
	}
	if !isProwlarrEnabled(cfg) {
		return RespondServiceUnavailable(c, "Prowlarr integration is disabled", "")
	}

	key := c.Params("key")
	if !s.validateDownloadKey(ctx, key) {
		return RespondUnauthorized(c, "Invalid key", "")
	}

	downloadURL := c.Query("url")
	safeTitle := c.Query("title")
	if downloadURL == "" {
		return RespondBadRequest(c, "Missing url parameter", "")
	}
	if safeTitle == "" {
		safeTitle = "unknown"
	}

	baseURL := resolveBaseURL(c, cfg.Stremio.BaseURL)

	safeFilename := safeTitle + ".nzb"
	nzbName := safeTitle

	// Short-circuit: return cached stream if already processed within TTL
	ttlHours := cfg.Stremio.NzbTTLHours
	completedStatus := database.QueueStatusCompleted
	if existing, err := s.queueRepo.ListQueueItems(ctx, &completedStatus, safeFilename, "", 1, 0, "updated_at", "desc"); err == nil && len(existing) > 0 {
		prev := existing[0]
		cacheValid := prev.StoragePath != nil && *prev.StoragePath != ""
		if cacheValid && ttlHours > 0 && prev.CompletedAt != nil {
			cacheValid = time.Since(*prev.CompletedAt) < time.Duration(ttlHours)*time.Hour
		}
		if cacheValid {
			if streams, err := s.buildStremioStreams(prev, baseURL, key, nzbName); err == nil && len(streams) > 0 {
				slog.InfoContext(ctx, "Returning cached Stremio stream for Prowlarr NZB",
					"nzb_name", nzbName)
				return c.Redirect(streams[0].URL, fiber.StatusFound)
			}
		}
	}

	// Write NZB to temp directory
	uploadDir := filepath.Join(os.TempDir(), "altmount-uploads")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		slog.ErrorContext(ctx, "Failed to create upload directory", "error", err)
		return RespondInternalError(c, "Failed to create upload directory", err.Error())
	}
	tempPath := filepath.Join(uploadDir, safeFilename)

	// Short-circuit: join existing active queue item
	if inQueue, _ := s.queueRepo.IsFileInQueue(ctx, tempPath); inQueue {
		if activeItems, err := s.queueRepo.ListQueueItems(ctx, nil, safeFilename, "", 1, 0, "updated_at", "desc"); err == nil && len(activeItems) > 0 {
			return s.waitAndRedirectToStream(c, activeItems[0].ID, baseURL, key, nzbName, 300)
		}
	}

	// Download NZB from Prowlarr
	prowlarrCfg := cfg.Stremio.Prowlarr
	client := prowlarr.NewClient(prowlarrCfg.Host, prowlarrCfg.APIKey)
	nzbData, err := client.DownloadNZB(ctx, downloadURL)
	if err != nil {
		slog.WarnContext(ctx, "Failed to download NZB from Prowlarr",
			"error", err, "title", safeTitle)
		return RespondServiceUnavailable(c, "Failed to download NZB from Prowlarr", err.Error())
	}

	if err := os.WriteFile(tempPath, nzbData, 0644); err != nil {
		slog.ErrorContext(ctx, "Failed to write NZB temp file", "error", err)
		return RespondInternalError(c, "Failed to write NZB temp file", err.Error())
	}

	// Add NZB to queue with high priority
	if s.importerService == nil {
		os.Remove(tempPath)
		slog.ErrorContext(ctx, "Importer service not available for Stremio addon play")
		return RespondServiceUnavailable(c, "Importer service not available", "")
	}

	var basePath *string
	if completeDir := cfg.SABnzbd.CompleteDir; completeDir != "" {
		basePath = &completeDir
	}

	priority := database.QueuePriorityHigh
	stremioCategory := "stremio"
	item, err := s.importerService.AddToQueue(ctx, tempPath, basePath, &stremioCategory, &priority)
	if err != nil {
		os.Remove(tempPath)
		slog.ErrorContext(ctx, "Failed to add Prowlarr NZB to queue", "error", err, "title", safeTitle)
		return RespondInternalError(c, "Failed to add NZB to queue", err.Error())
	}

	slog.InfoContext(ctx, "Prowlarr NZB queued for Stremio play",
		"queue_id", item.ID, "title", safeTitle)

	return s.waitAndRedirectToStream(c, item.ID, baseURL, key, nzbName, 300)
}

// waitAndRedirectToStream waits for a queue item to complete and 302-redirects to the first stream URL.
func (s *Server) waitAndRedirectToStream(c *fiber.Ctx, itemID int64, baseURL, downloadKey, nzbName string, timeoutSecs int) error {
	ctx := c.Context()

	subID, ch := s.progressBroadcaster.Subscribe()
	defer s.progressBroadcaster.Unsubscribe(subID)

	current, err := s.queueRepo.GetQueueItem(ctx, itemID)
	if err != nil || current == nil {
		return RespondServiceUnavailable(c, "Queue item not found", "")
	}

	redirectToFirst := func(item *database.ImportQueueItem) error {
		streams, err := s.buildStremioStreams(item, baseURL, downloadKey, nzbName)
		if err != nil || len(streams) == 0 {
			return RespondServiceUnavailable(c, "No streams available", "")
		}
		return c.Redirect(streams[0].URL, fiber.StatusFound)
	}

	switch current.Status {
	case database.QueueStatusCompleted:
		return redirectToFirst(current)
	case database.QueueStatusFailed:
		return RespondServiceUnavailable(c, "NZB processing failed", "")
	}

	timer := time.NewTimer(time.Duration(timeoutSecs) * time.Second)
	defer timer.Stop()

	for {
		select {
		case update, ok := <-ch:
			if !ok {
				return RespondServiceUnavailable(c, "Progress channel closed", "")
			}
			if update.QueueID != int(itemID) {
				continue
			}
			switch update.Status {
			case "completed":
				item, err := s.queueRepo.GetQueueItem(ctx, itemID)
				if err != nil {
					return RespondServiceUnavailable(c, "Failed to fetch queue item", "")
				}
				return redirectToFirst(item)
			case "failed":
				return RespondServiceUnavailable(c, "NZB processing failed", "")
			}
		case <-timer.C:
			return RespondServiceUnavailable(c, "Processing timed out",
				fmt.Sprintf("did not complete within %d seconds", timeoutSecs))
		}
	}
}

// validateDownloadKey returns true if key matches any user's hashed API key.
func (s *Server) validateDownloadKey(ctx context.Context, key string) bool {
	if s.userRepo == nil || key == "" {
		return false
	}
	users, err := s.userRepo.GetAllUsers(ctx)
	if err != nil {
		return false
	}
	for _, user := range users {
		if user.APIKey == nil || *user.APIKey == "" {
			continue
		}
		if hashAPIKey(*user.APIKey) == key {
			return true
		}
	}
	return false
}
