package newsnab

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/httpclient"
)

const (
	// capsCacheTTL bounds how long a successfully fetched caps document is reused.
	capsCacheTTL = time.Hour
	// capsFailureRetryTTL bounds how often a failing caps endpoint is retried,
	// so broken indexers are not hammered on every search.
	capsFailureRetryTTL = 10 * time.Minute
	// capsWaitBudget caps how much of a caller's search deadline may be spent
	// waiting on a caps lookup. Caps are an optimization, never a prerequisite:
	// once this elapses the search proceeds with unknown caps and legacy
	// unrestricted queries rather than starving behind a slow indexer.
	capsWaitBudget = 750 * time.Millisecond
	// capsFetchTimeout backstops a background caps fetch. The fetch runs on a
	// context detached from the caller's search deadline so that it keeps
	// warming the cache after the caller has given up; this bounds how long
	// such a goroutine may outlive that caller.
	capsFetchTimeout = 30 * time.Second
)

// Capabilities describes a Newsnab/Newznab indexer's advertised features from
// /api?t=caps. A nil *Capabilities means "unknown"; every accessor treats
// unknown as supported so behavior degrades gracefully to legacy queries.
type Capabilities struct {
	ServerName string `json:"server_name"`
	Categories []int  `json:"categories"`
	Search     bool   `json:"search"`
	// SearchParams lists the parameters the indexer advertises for t=search.
	SearchParams []string `json:"search_params,omitempty"`
	MovieSearch  bool     `json:"movie_search"`
	// MovieParams lists the parameters the indexer advertises for t=movie.
	MovieParams []string `json:"movie_params,omitempty"`
	TVSearch    bool     `json:"tv_search"`
	// TVParams lists the parameters the indexer advertises for t=tvsearch.
	TVParams []string `json:"tv_params,omitempty"`
}

// SearchType identifies the Newznab search function a parameter is checked
// against. Indexers advertise supportedParams per search function, not
// globally.
type SearchType string

const (
	// SearchTypeSearch is the generic t=search function.
	SearchTypeSearch SearchType = "search"
	// SearchTypeMovie is the t=movie function.
	SearchTypeMovie SearchType = "movie"
	// SearchTypeTV is the t=tvsearch function.
	SearchTypeTV SearchType = "tvsearch"
)

// Standard parameter sets assumed when a caps document omits the <searching>
// section entirely. Older indexers do this while still supporting the common
// parameters, so absence means unknown rather than unsupported. Mirrors the
// defaults Prowlarr's IndexerCapabilities applies.
var (
	standardSearchParams = []string{"q"}
	standardMovieParams  = []string{"q", "imdbid"}
	standardTVParams     = []string{"q", "imdbid", "tvdbid", "season", "ep"}
)

// supportsParam reports whether the indexer advertises the given Newznab
// parameter (q, imdbid, tvdbid, season, ep, ...) for the search type.
// Prowlarr-parity defaults: a search function that is available but declares
// no supportedParams attribute supports q only; an unavailable function
// supports nothing (callers consult Search/MovieSearch/TVSearch first); a nil
// Capabilities (unknown caps) behaves as "everything supported" so callers
// fall back to legacy unrestricted queries.
func (c *Capabilities) supportsParam(searchType SearchType, name string) bool {
	if c == nil {
		return true
	}
	var params []string
	switch searchType {
	case SearchTypeMovie:
		params = c.MovieParams
	case SearchTypeTV:
		params = c.TVParams
	default:
		params = c.SearchParams
	}
	if len(params) == 0 {
		return false
	}
	name = strings.ToLower(name)
	for _, p := range params {
		if p == name {
			return true
		}
	}
	return false
}

// filterCategories intersects requested categories with the indexer's declared
// category tree (top-level categories plus subcategories). Unknown caps or a
// completely disjoint tree leave the request unconstrained so a malformed caps
// response cannot accidentally narrow searches to nothing.
func filterCategories(caps *Capabilities, requested []int) []int {
	if caps == nil || len(caps.Categories) == 0 || len(requested) == 0 {
		return requested
	}

	declared := make(map[int]bool, len(caps.Categories))
	topLevel := make(map[int]bool)
	for _, id := range caps.Categories {
		declared[id] = true
		topLevel[id-id%1000] = true
	}

	filtered := make([]int, 0, len(requested))
	for _, id := range requested {
		if declared[id] || topLevel[id-id%1000] {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// getCaps returns cached indexer capabilities, refreshing them in the
// background when stale. A nil result means "unknown caps" and callers fall
// back to legacy unrestricted queries.
//
// The caps lookup never runs under the caller's lock and never inherits the
// caller's search deadline. A caller with nothing cached waits at most
// capsWaitBudget for an in-flight fetch before degrading to unknown caps, so a
// slow or hung caps endpoint costs one indexer its caps rather than starving
// every query aimed at it.
func (c *Client) getCaps(ctx context.Context, userAgent string) *Capabilities {
	caps, inflight := c.cachedCapsOrRefresh(ctx, userAgent)
	if inflight == nil {
		return caps
	}

	timer := time.NewTimer(capsWaitBudget)
	defer timer.Stop()

	select {
	case <-inflight:
		return c.cachedCaps()
	case <-timer.C:
		slog.DebugContext(ctx, "Newsnab caps lookup exceeded wait budget; using unrestricted queries",
			"indexer", c.config.Name, "wait_budget", capsWaitBudget)
		return nil
	case <-ctx.Done():
		return nil
	}
}

// cachedCapsOrRefresh reports the cached caps and, when a refresh is needed,
// starts (or joins) a single background fetch. It returns a non-nil channel
// only when the caller has nothing usable cached and may therefore benefit
// from briefly waiting. The mutex is never held across I/O.
func (c *Client) cachedCapsOrRefresh(ctx context.Context, userAgent string) (*Capabilities, <-chan struct{}) {
	c.capsMu.Lock()
	defer c.capsMu.Unlock()

	// A zero capsFetchedAt means nothing has ever been fetched. Otherwise a
	// cached hit stays valid for capsCacheTTL, and a cached failure (nil caps)
	// is respected for the shorter capsFailureRetryTTL.
	ttl := capsCacheTTL
	if c.caps == nil {
		ttl = capsFailureRetryTTL
	}
	if !c.capsFetchedAt.IsZero() && time.Since(c.capsFetchedAt) < ttl {
		return c.caps, nil
	}

	if c.capsInflight == nil {
		done := make(chan struct{})
		c.capsInflight = done
		go c.refreshCaps(ctx, userAgent, done)
	}

	// Serve stale-but-usable caps immediately rather than making the caller
	// wait on the revalidation it just triggered.
	if c.caps != nil {
		return c.caps, nil
	}
	return nil, c.capsInflight
}

// cachedCaps returns the currently cached caps without triggering a fetch.
func (c *Client) cachedCaps() *Capabilities {
	c.capsMu.Lock()
	defer c.capsMu.Unlock()
	return c.caps
}

// refreshCaps performs a caps fetch on a context detached from the caller's
// search deadline and stores the outcome. A failure caches nil, which both
// means "unknown caps" to callers and suppresses retries for
// capsFailureRetryTTL.
func (c *Client) refreshCaps(parent context.Context, userAgent string, done chan struct{}) {
	// Detached from the caller's cancellation but keeping its values, so the
	// fetch outlives the search that triggered it and warms the cache.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), capsFetchTimeout)
	defer cancel()

	caps, err := c.fetchCaps(ctx, userAgent)

	c.capsMu.Lock()
	c.caps = caps
	c.capsFetchedAt = time.Now()
	c.capsInflight = nil
	c.capsMu.Unlock()

	close(done)

	if err != nil {
		slog.WarnContext(ctx, "Newsnab caps lookup failed; falling back to unrestricted queries",
			"indexer", c.config.Name, "error", err)
	}
}

// fetchCaps performs the live /api?t=caps request.
func (c *Client) fetchCaps(ctx context.Context, userAgent string) (*Capabilities, error) {
	reqURL := fmt.Sprintf("%s/api?t=caps&apikey=%s", strings.TrimRight(c.config.URL, "/"), url.QueryEscape(c.config.APIKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("newsnab: create caps request failed: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("newsnab: caps request failed: %w", httpclient.RedactURLError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("newsnab: caps returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("newsnab: read caps body failed: %w", err)
	}

	caps := parseCaps(body)
	if caps == nil {
		return nil, fmt.Errorf("newsnab: caps response is not valid caps XML")
	}
	return caps, nil
}

// CheckCaps tests connectivity and returns indexer capabilities.
func (c *Client) CheckCaps(ctx context.Context, userAgent string) (*Capabilities, error) {
	return c.fetchCaps(ctx, userAgent)
}

type capsXMLSearch struct {
	Available string `xml:"available,attr"`
	// SupportedParams is the comma-separated parameter list advertised on the
	// search element itself, per the Newznab spec
	// (e.g. <tv-search available="yes" supportedParams="q,season,ep"/>).
	SupportedParams string `xml:"supportedParams,attr"`
}

type capsXML struct {
	XMLName xml.Name `xml:"caps"`
	Server  struct {
		Title string `xml:"title,attr"`
	} `xml:"server"`
	Searching struct {
		Search      capsXMLSearch `xml:"search"`
		MovieSearch capsXMLSearch `xml:"movie-search"`
		TVSearch    capsXMLSearch `xml:"tv-search"`
	} `xml:"searching"`
	Categories struct {
		Category []struct {
			ID     string `xml:"id,attr"`
			Subcat []struct {
				ID string `xml:"id,attr"`
			} `xml:"subcat"`
		} `xml:"category"`
	} `xml:"categories"`
}

// parseCaps decodes a caps document. It returns nil when the body is not a
// caps XML document at all (e.g. an indexer answered with RSS or HTML).
func parseCaps(body []byte) *Capabilities {
	var doc capsXML
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil
	}

	caps := &Capabilities{
		ServerName: doc.Server.Title,
	}

	// Older indexers omit the <searching> section entirely while still
	// supporting the common search types and parameters; absence means
	// unknown, so the standard parameter sets are assumed. When the section
	// is present, each search function is parsed with Prowlarr's defaults:
	// available but undeclared parameters mean q only, unavailable means the
	// function is not usable.
	searchingPresent := doc.Searching.Search.Available != "" ||
		doc.Searching.MovieSearch.Available != "" ||
		doc.Searching.TVSearch.Available != ""
	if !searchingPresent {
		caps.Search = true
		caps.SearchParams = standardSearchParams
		caps.MovieSearch = true
		caps.MovieParams = standardMovieParams
		caps.TVSearch = true
		caps.TVParams = standardTVParams
	} else {
		caps.Search = searchAvailable(doc.Searching.Search.Available)
		caps.SearchParams = advertisedParams(doc.Searching.Search, standardSearchParams)
		caps.MovieSearch = searchAvailable(doc.Searching.MovieSearch.Available)
		caps.MovieParams = advertisedParams(doc.Searching.MovieSearch, standardMovieParams)
		caps.TVSearch = searchAvailable(doc.Searching.TVSearch.Available)
		caps.TVParams = advertisedParams(doc.Searching.TVSearch, standardTVParams)
	}

	for _, cat := range doc.Categories.Category {
		if id, err := strconv.Atoi(cat.ID); err == nil {
			caps.Categories = append(caps.Categories, id)
		}
		for _, sub := range cat.Subcat {
			if id, err := strconv.Atoi(sub.ID); err == nil {
				caps.Categories = append(caps.Categories, id)
			}
		}
	}

	return caps
}

// advertisedParams returns the parameter list for an available search
// function. An available function without a supportedParams attribute is
// treated as supporting q only, matching Prowlarr's conservative reading of
// the spec; unavailable functions support nothing.
func advertisedParams(el capsXMLSearch, standard []string) []string {
	if !searchAvailable(el.Available) {
		return nil
	}
	if strings.TrimSpace(el.SupportedParams) == "" {
		return []string{"q"}
	}
	return parseSupportedParams(el.SupportedParams)
}

func searchAvailable(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1":
		return true
	default:
		return false
	}
}

func parseSupportedParams(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.ToLower(p))
		}
	}
	return out
}
