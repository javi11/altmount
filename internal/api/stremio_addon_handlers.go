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

// handleStremioManifest handles GET /stremio/:key/manifest.json
// Returns the Stremio addon manifest for addon installation.
func (s *Server) handleStremioManifest(c *fiber.Ctx) error {
	ctx := c.Context()

	if s.configManager == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "configuration not available"})
	}
	cfg := s.configManager.GetConfig()
	if cfg.Stremio.Enabled == nil || !*cfg.Stremio.Enabled {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Stremio integration is disabled"})
	}

	key := c.Params("key")
	if !s.validateDownloadKey(ctx, key) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid key"})
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
// Searches Prowlarr and returns play-URL options — no NZB download or queuing at this stage.
func (s *Server) handleStremioAddonStream(c *fiber.Ctx) error {
	ctx := c.Context()

	if s.configManager == nil {
		return c.JSON(fiber.Map{"streams": []any{}})
	}
	cfg := s.configManager.GetConfig()
	if cfg.Stremio.Enabled == nil || !*cfg.Stremio.Enabled {
		return c.JSON(fiber.Map{"streams": []any{}})
	}

	prowlarrCfg := cfg.Stremio.Prowlarr
	if prowlarrCfg.Enabled == nil || !*prowlarrCfg.Enabled {
		return c.JSON(fiber.Map{"streams": []any{}})
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
		return c.JSON(fiber.Map{"streams": []any{}})
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

	// Resolve base URL
	baseURL := strings.TrimRight(cfg.Stremio.BaseURL, "/")
	if baseURL == "" {
		baseURL = c.Protocol() + "://" + c.Hostname()
	}

	// Search Prowlarr — return play-URL options immediately, no download yet
	client := prowlarr.NewClient(prowlarrCfg.Host, prowlarrCfg.APIKey)
	results, err := client.Search(ctx, imdbID, prowlarrType, prowlarrCfg.Categories, season, episode)
	if err != nil {
		slog.WarnContext(ctx, "Prowlarr search failed", "error", err, "imdb_id", imdbID)
		return c.JSON(fiber.Map{"streams": []any{}})
	}
	if len(results) == 0 {
		slog.InfoContext(ctx, "No Prowlarr results found", "imdb_id", imdbID)
		return c.JSON(fiber.Map{"streams": []any{}})
	}

	// Apply language and quality filters
	langFilter := cfg.Stremio.Prowlarr.Languages
	qualFilter := cfg.Stremio.Prowlarr.Qualities
	filtered := results[:0]
	for _, r := range results {
		if !prowlarr.MatchesLanguage(r.Title, langFilter) {
			continue
		}
		if !prowlarr.MatchesQuality(r.Title, qualFilter) {
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
func (s *Server) handleStremioAddonPlay(c *fiber.Ctx) error {
	ctx := c.Context()

	if s.configManager == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("configuration not available")
	}
	cfg := s.configManager.GetConfig()
	if cfg.Stremio.Enabled == nil || !*cfg.Stremio.Enabled {
		return c.Status(fiber.StatusNotFound).SendString("Stremio integration is disabled")
	}

	prowlarrCfg := cfg.Stremio.Prowlarr
	if prowlarrCfg.Enabled == nil || !*prowlarrCfg.Enabled {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Prowlarr integration is disabled")
	}

	key := c.Params("key")
	if !s.validateDownloadKey(ctx, key) {
		return c.Status(fiber.StatusUnauthorized).SendString("invalid key")
	}

	downloadURL := c.Query("url")
	safeTitle := c.Query("title")
	if downloadURL == "" {
		return c.Status(fiber.StatusBadRequest).SendString("missing url parameter")
	}
	if safeTitle == "" {
		safeTitle = "unknown"
	}

	// Resolve base URL
	baseURL := strings.TrimRight(cfg.Stremio.BaseURL, "/")
	if baseURL == "" {
		baseURL = c.Protocol() + "://" + c.Hostname()
	}

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
		return c.Status(fiber.StatusInternalServerError).SendString("failed to create upload directory")
	}
	tempPath := filepath.Join(uploadDir, safeFilename)

	// Short-circuit: join existing active queue item
	if inQueue, _ := s.queueRepo.IsFileInQueue(ctx, tempPath); inQueue {
		if activeItems, err := s.queueRepo.ListQueueItems(ctx, nil, safeFilename, "", 1, 0, "updated_at", "desc"); err == nil && len(activeItems) > 0 {
			return s.waitAndRedirectToStream(c, activeItems[0].ID, baseURL, key, nzbName, 300)
		}
	}

	// Download NZB from Prowlarr
	client := prowlarr.NewClient(prowlarrCfg.Host, prowlarrCfg.APIKey)
	nzbData, err := client.DownloadNZB(ctx, downloadURL)
	if err != nil {
		slog.WarnContext(ctx, "Failed to download NZB from Prowlarr",
			"error", err, "title", safeTitle)
		return c.Status(fiber.StatusServiceUnavailable).SendString("failed to download NZB from Prowlarr")
	}

	if err := os.WriteFile(tempPath, nzbData, 0644); err != nil {
		slog.ErrorContext(ctx, "Failed to write NZB temp file", "error", err)
		return c.Status(fiber.StatusInternalServerError).SendString("failed to write NZB temp file")
	}

	// Add NZB to queue with high priority
	if s.importerService == nil {
		os.Remove(tempPath)
		slog.ErrorContext(ctx, "Importer service not available for Stremio addon play")
		return c.Status(fiber.StatusServiceUnavailable).SendString("importer service not available")
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
		return c.Status(fiber.StatusInternalServerError).SendString("failed to add NZB to queue")
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
		return c.Status(fiber.StatusServiceUnavailable).SendString("queue item not found")
	}

	redirectToFirst := func(item *database.ImportQueueItem) error {
		streams, err := s.buildStremioStreams(item, baseURL, downloadKey, nzbName)
		if err != nil || len(streams) == 0 {
			return c.Status(fiber.StatusServiceUnavailable).SendString("no streams available")
		}
		return c.Redirect(streams[0].URL, fiber.StatusFound)
	}

	switch current.Status {
	case database.QueueStatusCompleted:
		return redirectToFirst(current)
	case database.QueueStatusFailed:
		return c.Status(fiber.StatusServiceUnavailable).SendString("NZB processing failed")
	}

	timer := time.NewTimer(time.Duration(timeoutSecs) * time.Second)
	defer timer.Stop()

	for {
		select {
		case update, ok := <-ch:
			if !ok {
				return c.Status(fiber.StatusServiceUnavailable).SendString("progress channel closed")
			}
			if update.QueueID != int(itemID) {
				continue
			}
			switch update.Status {
			case "completed":
				item, err := s.queueRepo.GetQueueItem(ctx, itemID)
				if err != nil {
					return c.Status(fiber.StatusServiceUnavailable).SendString("failed to fetch queue item")
				}
				return redirectToFirst(item)
			case "failed":
				return c.Status(fiber.StatusServiceUnavailable).SendString("NZB processing failed")
			}
		case <-timer.C:
			return c.Status(fiber.StatusServiceUnavailable).
				SendString(fmt.Sprintf("processing did not complete within %d seconds", timeoutSecs))
		}
	}
}

// waitAndRespondAddon waits for a queue item to complete and returns Stremio streams.
// Always returns HTTP 200 — failures produce an empty streams array (Stremio shows "no streams").
func (s *Server) waitAndRespondAddon(c *fiber.Ctx, itemID int64, baseURL, downloadKey, nzbName string, timeoutSecs int) error {
	ctx := c.Context()

	subID, ch := s.progressBroadcaster.Subscribe()
	defer s.progressBroadcaster.Unsubscribe(subID)

	current, err := s.queueRepo.GetQueueItem(ctx, itemID)
	if err != nil || current == nil {
		return c.JSON(fiber.Map{"streams": []any{}})
	}

	switch current.Status {
	case database.QueueStatusCompleted:
		streams, err := s.buildStremioStreams(current, baseURL, downloadKey, nzbName)
		if err != nil || len(streams) == 0 {
			return c.JSON(fiber.Map{"streams": []any{}})
		}
		return c.JSON(fiber.Map{"streams": streams})
	case database.QueueStatusFailed:
		return c.JSON(fiber.Map{"streams": []any{}})
	}

	timer := time.NewTimer(time.Duration(timeoutSecs) * time.Second)
	defer timer.Stop()

	for {
		select {
		case update, ok := <-ch:
			if !ok {
				return c.JSON(fiber.Map{"streams": []any{}})
			}
			if update.QueueID != int(itemID) {
				continue
			}
			switch update.Status {
			case "completed":
				item, err := s.queueRepo.GetQueueItem(ctx, itemID)
				if err != nil {
					return c.JSON(fiber.Map{"streams": []any{}})
				}
				streams, err := s.buildStremioStreams(item, baseURL, downloadKey, nzbName)
				if err != nil || len(streams) == 0 {
					return c.JSON(fiber.Map{"streams": []any{}})
				}
				return c.JSON(fiber.Map{"streams": streams})
			case "failed":
				return c.JSON(fiber.Map{"streams": []any{}})
			}
		case <-timer.C:
			return c.JSON(fiber.Map{"streams": []any{}})
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
