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
	ServerName      string   `json:"server_name"`
	Categories      []int    `json:"categories"`
	Search          bool     `json:"search"`
	MovieSearch     bool     `json:"movie_search"`
	TVSearch        bool     `json:"tv_search"`
	SupportedParams []string `json:"supported_params,omitempty"`
}

// supportsParam reports whether the indexer advertises the given Newznab
// search parameter (imdbid, tvdbid, season, ep, ...). Indexers that do not
// declare supportedParams are treated as supporting every parameter.
func (c *Capabilities) supportsParam(name string) bool {
	if c == nil || len(c.SupportedParams) == 0 {
		return true
	}
	for _, p := range c.SupportedParams {
		if p == strings.ToLower(name) {
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
	SupportedParams string `xml:"supportedParams"`
}

// parseCaps decodes a caps document. It returns nil when the body is not a
// caps XML document at all (e.g. an indexer answered with RSS or HTML).
func parseCaps(body []byte) *Capabilities {
	var doc capsXML
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil
	}

	caps := &Capabilities{
		ServerName:      doc.Server.Title,
		Search:          searchAvailable(doc.Searching.Search.Available),
		MovieSearch:     searchAvailable(doc.Searching.MovieSearch.Available),
		TVSearch:        searchAvailable(doc.Searching.TVSearch.Available),
		SupportedParams: parseSupportedParams(doc.SupportedParams),
	}

	// Older indexers omit the <searching> section entirely while still
	// supporting every search type; absence means unknown, not unsupported.
	if doc.Searching.Search.Available == "" && doc.Searching.MovieSearch.Available == "" && doc.Searching.TVSearch.Available == "" {
		caps.Search = true
		caps.MovieSearch = true
		caps.TVSearch = true
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
