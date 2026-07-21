package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// rexumProvidersURL is the public data source mapping usenet hostnames to their
// upstream backbone (storage group). It has no CORS headers, so the browser
// cannot fetch it directly — this endpoint proxies and caches it.
const rexumProvidersURL = "https://usenet.rexum.space/api/providers"

// backboneCacheTTL is how long a fetched backbone list is served before refetch.
const backboneCacheTTL = 24 * time.Hour

// backboneFetchTimeout bounds a single outbound fetch of the rexum dataset.
const backboneFetchTimeout = 15 * time.Second

// BackboneEntry maps a provider hostname to its upstream backbone. The backbone
// name is a suitable value for a provider's storage_group setting: providers
// sharing a backbone share article availability.
type BackboneEntry struct {
	Host     string `json:"host"`
	Backbone string `json:"backbone"`
	Provider string `json:"provider"`
}

// rexumProvidersResponse is the subset of the rexum /api/providers payload we use.
type rexumProvidersResponse struct {
	Providers []struct {
		Name   string `json:"name"`
		Server []struct {
			NNTP     string `json:"nntp"`
			Backbone string `json:"backbone"`
		} `json:"server"`
	} `json:"providers"`
}

// backboneCache is a process-wide cache of the transformed backbone list. It is
// small (a few hundred entries) and rarely changes, so a single shared copy with
// a coarse mutex is sufficient.
var backboneCache = struct {
	mu        sync.Mutex
	entries   []BackboneEntry
	fetchedAt time.Time
}{}

// parseBackboneEntries transforms a raw rexum /api/providers payload into a slim,
// deduplicated host->backbone list. Entries missing a hostname or backbone are
// skipped; the first occurrence of a hostname wins.
func parseBackboneEntries(raw []byte) ([]BackboneEntry, error) {
	var resp rexumProvidersResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse backbone data: %w", err)
	}

	entries := make([]BackboneEntry, 0, len(resp.Providers))
	seen := make(map[string]struct{})
	for _, p := range resp.Providers {
		for _, s := range p.Server {
			host := strings.ToLower(strings.TrimSpace(s.NNTP))
			backbone := strings.TrimSpace(s.Backbone)
			if host == "" || backbone == "" {
				continue
			}
			if _, ok := seen[host]; ok {
				continue
			}
			seen[host] = struct{}{}
			entries = append(entries, BackboneEntry{
				Host:     host,
				Backbone: backbone,
				Provider: strings.TrimSpace(p.Name),
			})
		}
	}
	return entries, nil
}

// fetchBackboneEntries downloads and parses the rexum backbone dataset.
func fetchBackboneEntries(ctx context.Context) ([]BackboneEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rexumProvidersURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build backbone request: %w", err)
	}

	client := &http.Client{Timeout: backboneFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch backbone data: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("backbone data source returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // cap at 5 MiB
	if err != nil {
		return nil, fmt.Errorf("failed to read backbone data: %w", err)
	}

	return parseBackboneEntries(body)
}

// handleProviderBackbones returns the host->backbone list used by the frontend to
// autofill a provider's storage_group. The result is cached in-memory for
// backboneCacheTTL; on a fetch failure a previously cached copy is served.
//
//	@Summary		Get provider backbone map
//	@Description	Returns a cached mapping of usenet hostnames to their upstream backbone (storage group)
//	@Tags			providers
//	@Produce		json
//	@Success		200	{object}	map[string]interface{}
//	@Failure		503	{object}	map[string]interface{}
//	@Router			/providers/backbones [get]
func (s *Server) handleProviderBackbones(c *fiber.Ctx) error {
	backboneCache.mu.Lock()
	defer backboneCache.mu.Unlock()

	fresh := backboneCache.entries != nil && time.Since(backboneCache.fetchedAt) < backboneCacheTTL
	if fresh {
		return RespondSuccess(c, backboneCache.entries)
	}

	entries, err := fetchBackboneEntries(c.Context())
	if err != nil {
		// Serve stale data if we have any, rather than failing the UI.
		if backboneCache.entries != nil {
			return RespondSuccess(c, backboneCache.entries)
		}
		return RespondServiceUnavailable(c, "Failed to fetch provider backbone data", err.Error())
	}

	backboneCache.entries = entries
	backboneCache.fetchedAt = time.Now()
	return RespondSuccess(c, entries)
}
