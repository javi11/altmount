package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/httpclient"
	"github.com/javi11/altmount/internal/prowlarr"
)

// stremioDownloadIDPrefix marks queue items originating from the Stremio addon.
// Used by the cleanup service to identify and expire those items.
const stremioDownloadIDPrefix = "stremio:"

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
	if streamType != "movie" && streamType != "series" {
		return emptyStreamsResponse(c)
	}

	rawID, _ := url.PathUnescape(c.Params("id"))

	// Parse Stremio ID: tt1234567 (movie) or tt1234567:season:episode (series)
	imdbID, season, episode := parseStremioContentID(rawID)

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

	results, cachedItems, err := s.searchStremioReleases(ctx, cfg, streamType, prowlarrType, imdbID, season, episode)
	if err != nil || len(results) == 0 {
		return emptyStreamsResponse(c)
	}

	entries := buildStremioStreamEntries(results, cachedItems, cfg.Stremio.NzbTTLHours, time.Now(),
		baseURL, key, streamType, season, episode, imdbID)

	streams := make([]fiber.Map, 0, len(entries))
	for _, e := range entries {
		streams = append(streams, fiber.Map{
			"name":  e.Name,
			"title": e.Title,
			"url":   e.URL,
		})
	}

	return c.JSON(fiber.Map{"streams": streams})
}

// stremioPlayCandidate is one release a /play request may attempt. Candidates are
// produced in the same order the stream list presented, so the fallback chain and the
// list agree on what "the next release" means.
type stremioPlayCandidate struct {
	Title       string // raw Prowlarr title
	SafeTitle   string // sanitizeFilename(Title) -- the release key
	DownloadURL string
	Indexer     string
}

// normalizeTitleForMatching extracts an alphanumeric canonical representation of a title
// or file path, ignoring case, separators (dots, underscores, dashes, spaces), brackets,
// and file extensions (.nzb, .nzb.gz, .mkv, .mp4, etc.).
func normalizeTitleForMatching(s string) string {
	s = strings.ReplaceAll(s, "\\", "/")
	s = filepath.Base(s)
	for {
		orig := s
		s = strings.TrimSuffix(s, ".gz")
		s = strings.TrimSuffix(s, ".nzb")
		s = strings.TrimSuffix(s, ".mkv")
		s = strings.TrimSuffix(s, ".mp4")
		s = strings.TrimSuffix(s, ".avi")
		s = strings.TrimSuffix(s, ".iso")
		s = strings.TrimSuffix(s, ".rar")
		if s == orig {
			break
		}
	}

	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// stremioNzbPathMatches checks if an NZB path matches a candidate title regardless of
// case, punctuation, extensions, or directory wrappers using exact normalized equality.
func stremioNzbPathMatches(nzbPath, safeTitle string) bool {
	normA := normalizeTitleForMatching(nzbPath)
	normB := normalizeTitleForMatching(safeTitle)
	if normA == "" || normB == "" {
		return false
	}
	return normA == normB
}

// stremioCachedPredicate returns a test for "already imported and still usable":
// the item must have a storage path and, when a TTL is configured, have completed
// within it. Uses exact alphanumeric normalized matching to eliminate false positives.
func stremioCachedPredicate(cached []*database.ImportQueueItem, ttlHours int, now time.Time) func(safeTitle string) bool {
	normalizedCache := make(map[string]struct{}, len(cached)*3)
	for _, item := range cached {
		if item == nil || item.StoragePath == nil || *item.StoragePath == "" {
			continue
		}
		if ttlHours > 0 {
			if item.CompletedAt == nil || now.Sub(*item.CompletedAt) >= time.Duration(ttlHours)*time.Hour {
				continue
			}
		}
		if norm := normalizeTitleForMatching(item.NzbPath); norm != "" {
			normalizedCache[norm] = struct{}{}
		}
		if item.StoragePath != nil {
			if norm := normalizeTitleForMatching(*item.StoragePath); norm != "" {
				normalizedCache[norm] = struct{}{}
			}
		}
		if item.RelativePath != nil && *item.RelativePath != "" {
			if norm := normalizeTitleForMatching(*item.RelativePath); norm != "" {
				normalizedCache[norm] = struct{}{}
			}
		}
	}

	return func(safeTitle string) bool {
		target := normalizeTitleForMatching(safeTitle)
		if target == "" {
			return false
		}
		_, exists := normalizedCache[target]
		return exists
	}
}

// stremioFailedPredicate returns a test for "known not to work", combining the durable
// half (failed import_queue rows) with the process-local half (failures that leave no
// failed row -- see stremioFailureCache).
//
// Unlike the cached predicate this keys off UpdatedAt, because UpdateQueueItemStatus
// does not set completed_at on failure, and it does not require a storage path.
func stremioFailedPredicate(
	failed []*database.ImportQueueItem,
	memKeys map[string]struct{},
	ttlHours int,
	now time.Time,
) func(safeTitle string) bool {
	paths := make([]string, 0, len(failed))
	for _, item := range failed {
		if item == nil {
			continue
		}
		if ttlHours > 0 && now.Sub(item.UpdatedAt) >= time.Duration(ttlHours)*time.Hour {
			continue
		}
		paths = append(paths, item.NzbPath)
	}

	return func(safeTitle string) bool {
		if _, ok := memKeys[safeTitle]; ok {
			return true
		}
		for _, p := range paths {
			if stremioNzbPathMatches(p, safeTitle) {
				return true
			}
		}
		return false
	}
}

// filterStremioResults drops releases failing the configured language/quality filters,
// and those already known to have failed. Excluding failed releases here -- rather than
// demoting them -- is what stops a bad release being offered, and re-picked, forever.
func filterStremioResults(
	results []prowlarr.NZBResult,
	languages, qualities []string,
	isFailed func(safeTitle string) bool,
) []prowlarr.NZBResult {
	filtered := make([]prowlarr.NZBResult, 0, len(results))
	for _, r := range results {
		if !prowlarr.MatchesLanguage(r.Title, languages) {
			continue
		}
		if !prowlarr.MatchesQuality(r.Title, qualities) {
			continue
		}
		if isFailed != nil && isFailed(sanitizeFilename(r.Title)) {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// orderStremioResults stably reorders results so cached releases come first,
// preserving Prowlarr's relative order (publish date, newest first) within each group.
func orderStremioResults(results []prowlarr.NZBResult, isCached func(safeTitle string) bool) []prowlarr.NZBResult {
	ordered := make([]prowlarr.NZBResult, len(results))
	copy(ordered, results)
	sort.SliceStable(ordered, func(i, j int) bool {
		return isCached(sanitizeFilename(ordered[i].Title)) && !isCached(sanitizeFilename(ordered[j].Title))
	})
	return ordered
}

// stremioCandidates converts ordered Prowlarr results into play candidates.
func stremioCandidates(results []prowlarr.NZBResult, fallbackID string) []stremioPlayCandidate {
	cands := make([]stremioPlayCandidate, 0, len(results))
	for _, r := range results {
		safeTitle := sanitizeFilename(r.Title)
		if safeTitle == "" {
			safeTitle = fallbackID
		}
		cands = append(cands, stremioPlayCandidate{
			Title:       r.Title,
			SafeTitle:   safeTitle,
			DownloadURL: r.DownloadURL,
			Indexer:     r.Indexer,
		})
	}
	return cands
}

// nextStremioCandidate returns the index of the release to try after currentSafeTitle.
// When the current release is absent -- the Prowlarr list can change between /stream and
// /play -- it starts from the top rather than giving up. Reports false when exhausted.
func nextStremioCandidate(cands []stremioPlayCandidate, currentSafeTitle string, startAfter int) (int, bool) {
	if len(cands) == 0 {
		return 0, false
	}

	if startAfter < 0 {
		startAfter = -1
		for i, c := range cands {
			if c.SafeTitle == currentSafeTitle {
				startAfter = i
				break
			}
		}
		if startAfter < 0 {
			return 0, true
		}
	}

	next := startAfter + 1
	if next >= len(cands) {
		return 0, false
	}
	return next, true
}

// searchStremioReleases runs the Prowlarr search for a Stremio content id, applies the
// language/quality filters, drops releases known to have failed, and orders the result
// cached-first. It is the single source of truth for candidate ordering, shared by the
// stream list and the play fallback chain, so both agree on what "next" means.
func (s *Server) searchStremioReleases(
	ctx context.Context,
	cfg *config.Config,
	streamType, prowlarrType, imdbID string,
	season, episode int,
) ([]prowlarr.NZBResult, []*database.ImportQueueItem, error) {
	prowlarrCfg := cfg.Stremio.Prowlarr
	client := prowlarr.NewClient(
		prowlarrCfg.Host,
		prowlarrCfg.APIKey,
		httpclient.NewForExternal(cfg.Network, 30*time.Second),
	)

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
			results, err = client.SearchByTVDB(ctx, tvdbID, prowlarrType, prowlarrCfg.Categories, prowlarrCfg.Indexers, season, episode)
			if err != nil {
				slog.WarnContext(ctx, "Prowlarr TVDB search failed; falling back to IMDb search",
					"error", err, "imdb_id", imdbID, "tvdb_id", tvdbID)
				results = nil
			}
		}
	}

	if len(results) == 0 {
		results, err = client.Search(ctx, imdbID, prowlarrType, prowlarrCfg.Categories, prowlarrCfg.Indexers, season, episode)
		if err != nil {
			slog.WarnContext(ctx, "Prowlarr search failed", "error", err, "imdb_id", imdbID, "tvdb_id", tvdbID)
			return nil, nil, err
		}
	}

	if len(results) == 0 {
		slog.InfoContext(ctx, "No Prowlarr results found", "imdb_id", imdbID, "tvdb_id", tvdbID)
		return nil, nil, nil
	}

	now := time.Now()
	cachedItems, failedItems := s.loadStremioQueueState(ctx, cfg)

	total := len(results)
	results = filterStremioResults(results, prowlarrCfg.Languages, prowlarrCfg.Qualities,
		stremioFailedPredicate(failedItems, s.stremioFailures.Keys(stremioFailedTTL(cfg)),
			cfg.Stremio.FailedReleaseTTLHours, now))
	if len(results) < total {
		slog.InfoContext(ctx, "Filtered Prowlarr results",
			"imdb_id", imdbID, "total", total, "kept", len(results))
	}

	ordered := orderStremioResults(results, stremioCachedPredicate(cachedItems, cfg.Stremio.NzbTTLHours, now))
	return ordered, cachedItems, nil
}

// stremioFailedTTL is the age after which a recorded failure stops excluding a release.
func stremioFailedTTL(cfg *config.Config) time.Duration {
	return time.Duration(cfg.Stremio.FailedReleaseTTLHours) * time.Hour
}

// loadStremioQueueState fetches the cached and failed Stremio queue items. Both lookups
// are best-effort: a DB hiccup degrades badges and exclusion rather than failing search.
func (s *Server) loadStremioQueueState(ctx context.Context, cfg *config.Config) (cached, failed []*database.ImportQueueItem) {
	if s.queueRepo == nil {
		return nil, nil
	}

	reuseLibrary := false
	if cfg != nil {
		reuseLibrary = cfg.Stremio.EffectiveReuseLibraryReleases()
	}

	if items, err := s.queueRepo.GetCachedStremioQueueItems(ctx, reuseLibrary); err != nil {
		slog.WarnContext(ctx, "Failed to load cached Stremio items; continuing without cache badges",
			"error", err)
	} else {
		cached = items
	}

	if items, err := s.queueRepo.GetFailedStremioQueueItems(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to load failed Stremio items; continuing without exclusion",
			"error", err)
	} else {
		failed = items
	}

	return cached, failed
}

// stremioStreamEntry is a single Stremio stream option produced from a Prowlarr
// result, together with whether the underlying release is already cached/imported
// in AltMount.
type stremioStreamEntry struct {
	Name   string
	Title  string
	URL    string
	Cached bool
}

// buildStremioStreamEntries converts filtered Prowlarr results into ordered
// Stremio stream entries. Results whose release is already imported and still
// fresh (per the cached queue items and TTL) are badged "⚡ Cached" and stably
// sorted ahead of the rest, mirroring the aiostreams/Torrentio UX.
//
// The match/TTL logic mirrors the short-circuit in handleStremioAddonPlay so the
// badge is truthful: a cached entry is one /play resolves to an instant redirect.
// now is injected for deterministic TTL testing.
func buildStremioStreamEntries(
	results []prowlarr.NZBResult,
	cached []*database.ImportQueueItem,
	ttlHours int,
	now time.Time,
	baseURL, key, streamType string,
	season, episode int,
	fallbackID string,
) []stremioStreamEntry {
	isCached := stremioCachedPredicate(cached, ttlHours, now)

	// Order first, then build, so entries come out in final order without a second sort.
	results = orderStremioResults(results, isCached)

	entries := make([]stremioStreamEntry, 0, len(results))
	for _, r := range results {
		safeTitle := sanitizeFilename(r.Title)
		if safeTitle == "" {
			safeTitle = fallbackID
		}
		cachedHit := isCached(safeTitle)

		playURL := baseURL + "/stremio/" + key + "/play" +
			"?url=" + url.QueryEscape(r.DownloadURL) +
			"&title=" + url.QueryEscape(safeTitle) +
			"&type=" + url.QueryEscape(streamType)
		if r.Indexer != "" {
			playURL += "&indexer=" + url.QueryEscape(r.Indexer)
		}
		if fallbackID != "" {
			// The content id lets /play re-derive the candidate list to fall back on.
			playURL += "&id=" + url.QueryEscape(fallbackID)
		}
		if streamType == "series" && season > 0 && episode > 0 {
			playURL += "&season=" + url.QueryEscape(strconv.Itoa(season)) +
				"&episode=" + url.QueryEscape(strconv.Itoa(episode))
		}

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
		if cachedHit {
			// Prefix marks instantly-playable releases, aiostreams-style.
			streamName = "⚡ Cached · " + streamName
		}

		metaLine := fmt.Sprintf("💾 %.2f GB 🌐 %s", sizeGB, indexerLabel)
		entries = append(entries, stremioStreamEntry{
			Name:   streamName,
			Title:  fmt.Sprintf("%s\n%s", r.Title, metaLine),
			URL:    playURL,
			Cached: cachedHit,
		})
	}

	return entries
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
//	@Param			season	query	int		false	"Season number for selecting one episode from a season pack"
//	@Param			episode	query	int		false	"Episode number for selecting one episode from a season pack"
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
	imdbID := c.Query("id")
	streamType := c.Query("type")

	baseURL := resolveBaseURL(c, cfg.Stremio.BaseURL)
	selector := stremioEpisodeSelectorFromRequest(c)

	cand := stremioPlayCandidate{
		Title:       safeTitle,
		SafeTitle:   safeTitle,
		DownloadURL: downloadURL,
		Indexer:     c.Query("indexer"),
	}

	// Short-circuit: return cached stream if already processed within TTL. This runs
	// before the failed-release check so a genuinely playable file always wins over a
	// stale failure record.
	ttlHours := cfg.Stremio.NzbTTLHours
	normTarget := normalizeTitleForMatching(cand.SafeTitle)
	if normTarget != "" {
		if cachedItems, err := s.queueRepo.GetCachedStremioQueueItems(ctx, cfg.Stremio.EffectiveReuseLibraryReleases()); err == nil {
			for _, prev := range cachedItems {
				if prev == nil || prev.StoragePath == nil || *prev.StoragePath == "" {
					continue
				}
				if ttlHours > 0 && prev.CompletedAt != nil && time.Since(*prev.CompletedAt) >= time.Duration(ttlHours)*time.Hour {
					continue
				}
				normNzb := normalizeTitleForMatching(prev.NzbPath)
				normStorage := ""
				if prev.StoragePath != nil {
					normStorage = normalizeTitleForMatching(*prev.StoragePath)
				}
				normRel := ""
				if prev.RelativePath != nil {
					normRel = normalizeTitleForMatching(*prev.RelativePath)
				}

				if normNzb == normTarget || (normStorage != "" && normStorage == normTarget) || (normRel != "" && normRel == normTarget) {
					if streams, err := s.buildStremioStreams(ctx, prev, baseURL, key, cand.SafeTitle, selector); err == nil && len(streams) > 0 {
						slog.InfoContext(ctx, "Returning cached Stremio stream",
							"nzb_name", cand.SafeTitle, "matched_path", prev.NzbPath, "indexer", cand.Indexer)
						return c.Redirect(streams[0].URL, fiber.StatusFound)
					}
				}
			}
		}
	}

	return s.playStremioWithFallback(c, cfg, cand, playRequest{
		baseURL:    baseURL,
		key:        key,
		streamType: streamType,
		imdbID:     imdbID,
		selector:   selector,
	})
}

// playRequest carries the per-request context the fallback loop needs, keeping the
// loop signature readable.
type playRequest struct {
	baseURL    string
	key        string
	streamType string
	imdbID     string
	selector   *stremioEpisodeSelector
}

const (
	// stremioPlayBudget is the total wall clock a /play request may spend across all
	// attempts. Unchanged from the original single-attempt timeout, so client
	// behaviour is unaffected.
	stremioPlayBudget = 300 * time.Second
	// stremioMinAttemptBudget is the minimum time left worth starting another attempt.
	stremioMinAttemptBudget = 20 * time.Second
)

// playStremioWithFallback queues the requested release and waits for it, advancing to
// the next candidate release when one fails. Without this, a release that fails is
// simply re-downloaded and re-queued on every play, forever.
func (s *Server) playStremioWithFallback(
	c *fiber.Ctx,
	cfg *config.Config,
	cand stremioPlayCandidate,
	req playRequest,
) error {
	ctx := c.Context()

	// Candidates are resolved lazily: the happy path must not pay for a second
	// Prowlarr search.
	var (
		cands       []stremioPlayCandidate
		candsLoaded bool
		curIdx      = -1
	)
	loadCandidates := func() {
		if candsLoaded {
			return
		}
		candsLoaded = true
		if req.imdbID == "" {
			return
		}
		prowlarrType := "search"
		switch req.streamType {
		case "movie":
			prowlarrType = "movie"
		case "series":
			prowlarrType = "tvsearch"
		}
		season, episode := 0, 0
		if req.selector != nil {
			season, episode = req.selector.Season, req.selector.Episode
		}
		results, _, err := s.searchStremioReleases(ctx, cfg, req.streamType, prowlarrType, req.imdbID, season, episode)
		if err != nil {
			slog.WarnContext(ctx, "Stremio fallback search failed", "error", err, "imdb_id", req.imdbID)
			return
		}
		cands = stremioCandidates(results, req.imdbID)
	}

	// advance moves to the next candidate, returning false when fallback is disabled,
	// unavailable, or exhausted.
	advance := func() bool {
		if cfg.Stremio.EffectiveMaxFallbackReleases() == 0 || req.imdbID == "" {
			return false
		}
		loadCandidates()
		next, ok := nextStremioCandidate(cands, cand.SafeTitle, curIdx)
		if !ok {
			return false
		}
		curIdx = next
		cand = cands[next]
		slog.InfoContext(ctx, "Falling back to next Stremio release",
			"imdb_id", req.imdbID, "title", cand.SafeTitle)
		return true
	}

	deadline := time.Now().Add(stremioPlayBudget)
	maxAttempts := 1 + cfg.Stremio.EffectiveMaxFallbackReleases()
	attempts := 0

	// A stale client-cached stream list can point straight at a release we already know
	// is bad. Skip the download entirely and start from the next candidate.
	if s.stremioIsFailed(ctx, cfg, cand.SafeTitle) {
		slog.InfoContext(ctx, "Skipping known-failed Stremio release", "title", cand.SafeTitle)
		if !advance() {
			return RespondServiceUnavailable(c, "No playable release found", "release previously failed")
		}
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		remaining := time.Until(deadline)
		if attempt > 1 && remaining < stremioMinAttemptBudget {
			break
		}
		attempts++

		itemID, err := s.enqueueStremioRelease(ctx, cfg, cand, req.streamType)
		if err != nil {
			// Pre-queue failure (dead Prowlarr URL, malformed NZB): no queue row will
			// ever exist, so the in-memory cache is the only place this can be recorded.
			slog.WarnContext(ctx, "Failed to prepare Stremio NZB stream",
				"error", err, "title", cand.SafeTitle)
			s.stremioFailures.Record(cand.SafeTitle)
			if !advance() {
				return RespondServiceUnavailable(c, "Failed to prepare NZB stream", err.Error())
			}
			continue
		}

		out := s.waitForStream(ctx, itemID, req.baseURL, req.key, cand.SafeTitle, req.selector, remaining)
		switch out.Kind {
		case streamOutcomeReady:
			// Self-healing: a release that plays is no longer excluded.
			s.stremioFailures.Forget(cand.SafeTitle)
			return c.Redirect(out.StreamURL, fiber.StatusFound)

		case streamOutcomeNoStreams:
			// The row is 'completed', so GetFailedStremioQueueItems will never see it
			// and it would otherwise keep its "⚡ Cached" badge.
			s.stremioFailures.Record(cand.SafeTitle)
			if !advance() {
				return respondStreamOutcome(c, out)
			}

		case streamOutcomeFailed:
			// The failed import_queue row already records this durably.
			if !advance() {
				return respondStreamOutcome(c, out)
			}

		default:
			// Ambiguous, timeout and unavailable are not evidence the release is bad.
			return respondStreamOutcome(c, out)
		}
	}

	slog.WarnContext(ctx, "No playable Stremio release found",
		"imdb_id", req.imdbID, "attempts", attempts, "candidates", len(cands))
	return RespondServiceUnavailable(c, "No playable release found",
		fmt.Sprintf("tried %d release(s), none playable", attempts))
}

// stremioIsFailed reports whether a release is currently excluded, consulting both the
// durable (import_queue) and process-local halves of the exclusion set.
func (s *Server) stremioIsFailed(ctx context.Context, cfg *config.Config, safeTitle string) bool {
	if s.stremioFailures.Has(safeTitle, stremioFailedTTL(cfg)) {
		return true
	}
	if s.queueRepo == nil {
		return false
	}
	failed, err := s.queueRepo.GetFailedStremioQueueItems(ctx)
	if err != nil {
		return false
	}
	return stremioFailedPredicate(failed, nil, cfg.Stremio.FailedReleaseTTLHours, time.Now())(safeTitle)
}

// enqueueStremioRelease downloads the NZB and adds it to the import queue, coalescing
// concurrent callers for the same release so it is downloaded once.
//
// It returns as soon as the queue item exists. The wait deliberately stays outside the
// singleflight group: holding the group for the whole import would serialize every
// concurrent viewer of the release behind one request.
func (s *Server) enqueueStremioRelease(
	ctx context.Context,
	cfg *config.Config,
	cand stremioPlayCandidate,
	streamType string,
) (int64, error) {
	safeFilename := cand.SafeTitle + ".nzb"
	ttlHours := cfg.Stremio.NzbTTLHours

	var indexerPtr *string
	if cand.Indexer != "" {
		indexer := cand.Indexer
		indexerPtr = &indexer
	}

	v, err, _ := s.stremioPlayGroup.Do(safeFilename, func() (interface{}, error) {
		// Serialized per title: reuse an in-flight or TTL-fresh import instead of re-downloading.
		if items, e := s.queueRepo.ListQueueItems(ctx, nil, safeFilename, "", 1, 0, "updated_at", "desc"); e == nil && len(items) > 0 {
			it := items[0]
			switch it.Status {
			case database.QueueStatusPending, database.QueueStatusProcessing, database.QueueStatusPaused:
				return it.ID, nil
			case database.QueueStatusCompleted:
				reusable := it.StoragePath != nil && *it.StoragePath != ""
				if reusable && ttlHours > 0 && it.CompletedAt != nil {
					reusable = time.Since(*it.CompletedAt) < time.Duration(ttlHours)*time.Hour
				}
				if reusable {
					return it.ID, nil
				}
			}
		}

		if s.importerService == nil {
			return nil, fmt.Errorf("importer service not available")
		}

		// Detach from the caller's request so one client disconnecting won't abort shared work.
		workCtx := context.WithoutCancel(ctx)

		// Unique per-request staging dir so concurrent plays never share a temp file.
		uploadDir := filepath.Join(os.TempDir(), "altmount-uploads")
		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create upload directory: %w", err)
		}
		stageDir, err := os.MkdirTemp(uploadDir, "play-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create staging directory: %w", err)
		}
		// Importer moves the NZB out on success; this clears the staged file / empty dir.
		defer os.RemoveAll(stageDir)
		tempPath := filepath.Join(stageDir, safeFilename)

		// Download NZB from Prowlarr
		prowlarrCfg := cfg.Stremio.Prowlarr
		client := prowlarr.NewClient(
			prowlarrCfg.Host,
			prowlarrCfg.APIKey,
			httpclient.NewForExternal(cfg.Network, httpclient.LongTimeout),
		)
		nzbData, err := client.DownloadNZB(workCtx, cand.DownloadURL)
		if err != nil {
			return nil, fmt.Errorf("failed to download NZB from Prowlarr: %w", err)
		}
		if err := os.WriteFile(tempPath, nzbData, 0644); err != nil {
			return nil, fmt.Errorf("failed to write NZB temp file: %w", err)
		}

		var basePath *string
		if completeDir := cfg.SABnzbd.CompleteDir; completeDir != "" {
			basePath = &completeDir
		}

		priority := database.QueuePriorityHigh
		// Map Stremio stream type to Newznab category name so downloads land in the
		// correct folder (matches default SABnzbd category config).
		category := "Movies"
		if streamType == "series" {
			category = "TV"
		}
		stremioDownloadID := stremioDownloadIDPrefix + uuid.NewString()
		item, err := s.importerService.AddToQueue(workCtx, tempPath, basePath, &category, &priority, nil, &stremioDownloadID, indexerPtr)
		if err != nil {
			return nil, fmt.Errorf("failed to add NZB to queue: %w", err)
		}

		slog.InfoContext(ctx, "Prowlarr NZB queued for Stremio play",
			"queue_id", item.ID, "title", cand.SafeTitle)
		return item.ID, nil
	})
	if err != nil {
		return 0, err
	}

	itemID, ok := v.(int64)
	if !ok {
		return 0, fmt.Errorf("unexpected play result type %T", v)
	}
	return itemID, nil
}

// streamOutcomeKind classifies how a wait for a queue item ended. The distinction
// drives fallback: only definitive, release-specific failures justify trying another
// release.
type streamOutcomeKind int

const (
	// streamOutcomeReady: a playable stream URL is available.
	streamOutcomeReady streamOutcomeKind = iota
	// streamOutcomeFailed: the import failed. A 'failed' row now exists in
	// import_queue, so exclusion is already durably recorded.
	streamOutcomeFailed
	// streamOutcomeNoStreams: the import completed but produced nothing playable.
	// The row is 'completed', so this must be recorded in the in-memory cache.
	streamOutcomeNoStreams
	// streamOutcomeAmbiguous: a multi-episode pack with no episode selector. A client
	// problem, not a bad release -- never falls back, never recorded.
	streamOutcomeAmbiguous
	// streamOutcomeTimeout: still importing. Never falls back (the item is still
	// running and a second high-priority import would contend for the same connection
	// pool) and never recorded.
	streamOutcomeTimeout
	// streamOutcomeUnavailable: the wait could not be performed at all.
	streamOutcomeUnavailable
)

// streamOutcome is the result of waiting on a queue item.
type streamOutcome struct {
	Kind      streamOutcomeKind
	StreamURL string
	Detail    string
}

// waitForStream waits for a queue item to become streamable and classifies the result.
// Unlike waitAndRedirectToStream it never writes to the HTTP response, so the caller
// can decide between redirecting and falling back to another release.
func (s *Server) waitForStream(
	ctx context.Context,
	itemID int64,
	baseURL, downloadKey, nzbName string,
	selector *stremioEpisodeSelector,
	timeout time.Duration,
) streamOutcome {
	// Subscribe before reading status so an event fired between the two is not missed.
	subID, ch := s.progressBroadcaster.Subscribe()
	defer s.progressBroadcaster.Unsubscribe(subID)

	current, err := s.queueRepo.GetQueueItem(ctx, itemID)
	if err != nil || current == nil {
		return streamOutcome{Kind: streamOutcomeUnavailable, Detail: "queue item not found"}
	}

	firstStream := func(item *database.ImportQueueItem) streamOutcome {
		streams, err := s.buildStremioStreams(ctx, item, baseURL, downloadKey, nzbName, selector)
		if err != nil {
			if errors.Is(err, errStremioEpisodeAmbiguous) {
				return streamOutcome{Kind: streamOutcomeAmbiguous}
			}
			return streamOutcome{Kind: streamOutcomeNoStreams, Detail: err.Error()}
		}
		if len(streams) == 0 {
			return streamOutcome{Kind: streamOutcomeNoStreams, Detail: "no media files in release"}
		}
		return streamOutcome{Kind: streamOutcomeReady, StreamURL: streams[0].URL}
	}

	switch current.Status {
	case database.QueueStatusCompleted:
		return firstStream(current)
	case database.QueueStatusFailed:
		detail := ""
		if current.ErrorMessage != nil {
			detail = *current.ErrorMessage
		}
		return streamOutcome{Kind: streamOutcomeFailed, Detail: detail}
	default:
		// If the item is already processing and has a storage path, the streamable
		// event fired before we subscribed — use it immediately.
		if current.StoragePath != nil && *current.StoragePath != "" {
			if out := firstStream(current); out.Kind == streamOutcomeReady {
				return out
			}
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case update, ok := <-ch:
			if !ok {
				return streamOutcome{Kind: streamOutcomeUnavailable, Detail: "progress channel closed"}
			}
			if update.QueueID != int(itemID) {
				continue
			}
			switch update.Status {
			case "streamable":
				// Serve as soon as the file is accessible in the VFS — before post-processing.
				if update.StoragePath != "" {
					fakeItem := &database.ImportQueueItem{ID: itemID, StoragePath: &update.StoragePath}
					if out := firstStream(fakeItem); out.Kind == streamOutcomeReady {
						return out
					}
				}
				// No media files yet — fall through to wait for completed.
			case "completed":
				item, err := s.queueRepo.GetQueueItem(ctx, itemID)
				if err != nil {
					return streamOutcome{Kind: streamOutcomeUnavailable, Detail: "failed to fetch queue item"}
				}
				return firstStream(item)
			case "failed":
				return streamOutcome{Kind: streamOutcomeFailed}
			}
		case <-timer.C:
			return streamOutcome{
				Kind:   streamOutcomeTimeout,
				Detail: fmt.Sprintf("did not complete within %s", timeout),
			}
		}
	}
}

// respondStreamOutcome writes the HTTP response for a non-ready outcome.
func respondStreamOutcome(c *fiber.Ctx, out streamOutcome) error {
	switch out.Kind {
	case streamOutcomeReady:
		return c.Redirect(out.StreamURL, fiber.StatusFound)
	case streamOutcomeAmbiguous:
		return respondEpisodeAmbiguous(c)
	case streamOutcomeFailed:
		return RespondServiceUnavailable(c, "NZB processing failed", out.Detail)
	case streamOutcomeNoStreams:
		return RespondServiceUnavailable(c, "No streams available", out.Detail)
	case streamOutcomeTimeout:
		return RespondServiceUnavailable(c, "Processing timed out", out.Detail)
	default:
		return RespondServiceUnavailable(c, "Failed to prepare NZB stream", out.Detail)
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
		if subtle.ConstantTimeCompare([]byte(auth.HashAPIKey(*user.APIKey)), []byte(key)) == 1 {
			return true
		}
	}
	return false
}

// prowlarrIndexersRequest carries optional connection overrides so the config UI
// can list indexers before the Prowlarr settings are saved.
type prowlarrIndexersRequest struct {
	Host   string `json:"host"`
	APIKey string `json:"api_key"`
}

// handleListProwlarrIndexers returns the usenet indexers configured in Prowlarr
// so the Stremio config UI can present them for selection. Host and API key may
// be supplied in the body (to test unsaved values) or fall back to saved config.
//
//	@Summary		List Prowlarr indexers
//	@Description	Returns the usenet indexers configured in Prowlarr for the Stremio addon.
//	@Tags			Config
//	@Accept			json
//	@Produce		json
//	@Param			body	body		prowlarrIndexersRequest	false	"Optional Prowlarr connection overrides"
//	@Success		200		{object}	APIResponse
//	@Failure		400		{object}	APIResponse
//	@Security		BearerAuth
//	@Router			/prowlarr/indexers [post]
func (s *Server) handleListProwlarrIndexers(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration not available", "")
	}

	var req prowlarrIndexersRequest
	// Body is optional; ignore parse errors and fall back to saved config.
	_ = c.BodyParser(&req)

	cfg := s.configManager.GetConfig()
	host := strings.TrimSpace(req.Host)
	if host == "" {
		host = cfg.Stremio.Prowlarr.Host
	}
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		apiKey = cfg.Stremio.Prowlarr.APIKey
	}

	if host == "" || apiKey == "" {
		return RespondValidationError(c, "Prowlarr host and API key are required", "")
	}

	client := prowlarr.NewClient(host, apiKey, httpclient.NewForExternal(cfg.Network, 30*time.Second))
	indexers, err := client.GetIndexers(c.Context())
	if err != nil {
		return RespondBadRequest(c, "Failed to list Prowlarr indexers", err.Error())
	}

	return RespondSuccess(c, indexers)
}
