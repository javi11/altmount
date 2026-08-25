package newsnab

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/httpclient"
)

// IndexerConfig holds configuration for a single direct Newsnab/Newznab indexer.
type IndexerConfig struct {
	ID             string `yaml:"id" mapstructure:"id" json:"id"`
	Name           string `yaml:"name" mapstructure:"name" json:"name"`
	URL            string `yaml:"url" mapstructure:"url" json:"url"`
	APIKey         string `yaml:"api_key" mapstructure:"api_key" json:"api_key"`
	Categories     []int  `yaml:"categories" mapstructure:"categories" json:"categories"`
	Weight         int    `yaml:"weight" mapstructure:"weight" json:"weight"`
	TimeoutSeconds int    `yaml:"timeout_seconds" mapstructure:"timeout_seconds" json:"timeout_seconds"`
	Enabled        bool   `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
}

// Result represents a single search result from a Newsnab indexer.
type Result struct {
	Title       string
	DownloadURL string
	Size        int64
	PublishDate time.Time
	Indexer     string
	IndexerID   string
	GUID        string
	Category    int
	// ByIDSearch reports whether this release was matched by the indexer via
	// an identifier query (imdbid/tvdbid) rather than a keyword query. Such
	// results are trusted downstream, mirroring Prowlarr/Radarr semantics.
	ByIDSearch bool
}

// DownloadNZB downloads an NZB from this configured indexer without sending
// credentials belonging to another provider.
func (c *Client) DownloadNZB(ctx context.Context, downloadURL string, userAgent string) ([]byte, error) {
	if err := httpclient.ValidateDownloadURL(downloadURL); err != nil {
		return nil, fmt.Errorf("newsnab: refusing download from %s: %w", c.config.Name, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("newsnab: create download request failed: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	client := *c.httpClient
	client.CheckRedirect = httpclient.SafeDownloadCheckRedirect(10)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("newsnab: download request failed (%s): %w", c.config.Name, httpclient.RedactURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("newsnab: download returned HTTP %d (%s)", resp.StatusCode, c.config.Name)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
}

// Client interacts with a Newsnab-compatible indexer API.
type Client struct {
	config     IndexerConfig
	httpClient *http.Client

	capsMu        sync.Mutex
	caps          *Capabilities
	capsFetchedAt time.Time
	// capsInflight is non-nil while a background caps refresh is running and
	// is closed when it completes, single-flighting concurrent lookups.
	capsInflight chan struct{}

	// idMu guards idSearchFailures, the learned negative cache for identifier
	// searches: indexers that answer a bare imdbid/tvdbid query with zero
	// rows (no identifier mappings) are not retried with identifiers until
	// the entry expires.
	idMu              sync.Mutex
	idSearchFailures  map[string]time.Time
}

// Parameters usable as negative-cache keys for identifier searches.
const (
	idParamImdb = "imdbid"
	idParamTVDB = "tvdbid"
)

// idSearchNegativeTTL bounds how long a zero-result bare identifier search
// suppresses identifier queries for an indexer. It matches the caps cache
// TTL so both learned views of an indexer expire together.
const idSearchNegativeTTL = time.Hour

// idSearchBroken reports whether a recent bare identifier search for this
// indexer returned zero results, meaning the indexer likely cannot resolve
// identifiers at all (e.g. it has no IMDb mappings) and keyword searches
// should be preferred until the entry expires.
func (c *Client) idSearchBroken(param string) bool {
	c.idMu.Lock()
	defer c.idMu.Unlock()
	at, ok := c.idSearchFailures[param]
	return ok && time.Since(at) < idSearchNegativeTTL
}

// markIDSearchBroken records that a bare identifier search returned zero
// results for this indexer.
func (c *Client) markIDSearchBroken(param string) {
	c.idMu.Lock()
	defer c.idMu.Unlock()
	if c.idSearchFailures == nil {
		c.idSearchFailures = make(map[string]time.Time)
	}
	c.idSearchFailures[param] = time.Now()
}

// cloneWithout returns a copy of params with the given keys removed.
func cloneWithout(params url.Values, keys ...string) url.Values {
	out := make(url.Values, len(params))
	for k, vs := range params {
		drop := false
		for _, key := range keys {
			if k == key {
				drop = true
				break
			}
		}
		if !drop {
			out[k] = vs
		}
	}
	return out
}

// NewClient creates a new Newsnab client.
func NewClient(cfg IndexerConfig, httpClient *http.Client) *Client {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	} else if httpClient.Timeout != timeout {
		cloned := *httpClient
		cloned.Timeout = timeout
		httpClient = &cloned
	}
	return &Client{
		config:     cfg,
		httpClient: httpClient,
	}
}

// Name returns the configured name of the indexer.
func (c *Client) Name() string {
	return c.config.Name
}

// ID returns the configured ID of the indexer.
func (c *Client) ID() string {
	return c.config.ID
}

// newsnabJSONResponse represents the JSON payload returned when o=json is passed.
type newsnabJSONResponse struct {
	Channel struct {
		Title string          `json:"title"`
		Item  json.RawMessage `json:"item"`
	} `json:"channel"`
}

type newsnabJSONAttributes struct {
	URL    string `json:"url"`
	Length string `json:"length"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	Value  string `json:"value"`
}

type newsnabJSONItem struct {
	Title     string          `json:"title"`
	Link      string          `json:"link"`
	GUID      json.RawMessage `json:"guid"`
	PubDate   string          `json:"pubDate"`
	Enclosure struct {
		Attributes newsnabJSONAttributes `json:"@attributes"`
		URL        string                `json:"_url"`
		Length     string                `json:"_length"`
		Type       string                `json:"_type"`
	} `json:"enclosure"`
	Attr []struct {
		Attributes newsnabJSONAttributes `json:"@attributes"`
		Name       string                `json:"_name"`
		Value      string                `json:"_value"`
	} `json:"attr"`
}

// newsnabXMLRSS represents the RSS XML payload returned by Newznab.
type newsnabXMLRSS struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Title string           `xml:"title"`
		Items []newsnabXMLItem `xml:"item"`
	} `xml:"channel"`
}

type newsnabXMLItem struct {
	Title     string `xml:"title"`
	Link      string `xml:"link"`
	PubDate   string `xml:"pubDate"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
		Type   string `xml:"type,attr"`
	} `xml:"enclosure"`
	GUID struct {
		Value       string `xml:",chardata"`
		IsPermaLink string `xml:"isPermaLink,attr"`
	} `xml:"guid"`
	Attrs []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"attr"`
}

// SearchMovie searches for movie releases by IMDb ID, degrading to keyword
// queries when the indexer's caps do not advertise movie-search or imdbid
// support — or when a learned probe showed identifier queries return nothing
// for this indexer — mirroring Prowlarr/Radarr per-indexer behavior.
func (c *Client) SearchMovie(ctx context.Context, imdbID, title string, categories []int, userAgent string) ([]Result, error) {
	cleanIMDB := strings.TrimPrefix(imdbID, "tt")
	caps := c.getCaps(ctx, userAgent)

	params := url.Values{}
	byID := true
	switch {
	case caps != nil && !caps.MovieSearch:
		params.Set("t", "search")
		params.Set("q", queryFallback(imdbID, title))
		byID = false
	case cleanIMDB != "" && !c.idSearchBroken(idParamImdb) && caps.supportsParam(SearchTypeMovie, "imdbid"):
		params.Set("t", "movie")
		params.Set("imdbid", cleanIMDB)
	default:
		params.Set("t", "movie")
		params.Set("q", queryFallback(imdbID, title))
		byID = false
	}

	cats := filterCategories(caps, c.resolveCategories(categories, []int{2000, 2010, 2030, 2040, 2045, 2060}))
	setCategories(params, cats)

	results, err := c.executeSearch(ctx, params, userAgent, byID)
	if err != nil {
		return nil, err
	}
	if len(results) > 0 || !byID {
		return results, nil
	}

	// Some indexers accept the identifier but combine it with categories so
	// strictly that the pair returns nothing (NZBgeek answers a movie
	// identifier queried with TV categories and zero rows). Retry with the
	// identifier alone; the identifier still pins the movie exactly, so
	// identifier trust is preserved.
	if len(cats) > 0 {
		degraded := cloneWithout(params, "cat")
		retry, err := c.executeSearch(ctx, degraded, userAgent, byID)
		if err != nil {
			slog.DebugContext(ctx, "Newsnab category-degraded retry failed",
				"indexer", c.config.Name, "error", err)
			return results, nil
		}
		if len(retry) > 0 {
			slog.DebugContext(ctx, "Newsnab search succeeded after dropping category filter",
				"indexer", c.config.Name, "results", len(retry))
			return retry, nil
		}
	}

	// A bare identifier search that still returns nothing means this indexer
	// likely cannot resolve identifiers at all (e.g. no IMDb mappings).
	// Remember that for a while and answer this request with a keyword search.
	c.markIDSearchBroken(idParamImdb)

	keyword := cloneWithout(params, idParamImdb, "cat")
	keyword.Set("q", queryFallback(imdbID, title))
	keywordResults, err := c.executeSearch(ctx, keyword, userAgent, false)
	if err != nil {
		slog.DebugContext(ctx, "Newsnab keyword fallback after empty identifier search failed",
			"indexer", c.config.Name, "error", err)
		return results, nil
	}
	if len(keywordResults) > 0 {
		slog.DebugContext(ctx, "Newsnab identifier search unsupported; keyword fallback succeeded",
			"indexer", c.config.Name, "results", len(keywordResults))
	}
	return keywordResults, nil
}

// SearchTV searches for TV episode releases by IMDB ID, TVDB ID, title, season, and episode.
// Follows Prowlarr/Sonarr parameter prioritization, skipping parameters the
// indexer does not advertise via caps:
// Priority 1: tvdbid
// Priority 2: imdbid
// Priority 3: q=title (fallback text query)
//
// When a narrowed query returns zero rows the search degrades one rung at a
// time (drop ep, then season, then categories): some indexers advertise
// episode filtering but return nothing for content their filters cannot
// parse — absolute-numbered anime releases are the common case. Degraded
// identifier matches lose episode precision, so they are marked untrusted
// and re-validated by the caller's media matching. A bare identifier search
// that still comes back empty marks identifier queries unsupported for this
// indexer (negative cache) and answers with a keyword search instead.
func (c *Client) SearchTV(ctx context.Context, imdbID, tvdbID, title string, season, episode int, categories []int, userAgent string) ([]Result, error) {
	cleanIMDB := strings.TrimPrefix(imdbID, "tt")
	caps := c.getCaps(ctx, userAgent)

	tvSearchSupported := caps == nil || caps.TVSearch
	params := url.Values{}
	byID := true
	if tvSearchSupported {
		params.Set("t", "tvsearch")
	} else {
		params.Set("t", "search")
		byID = false
	}

	switch {
	case tvSearchSupported && tvdbID != "" && !c.idSearchBroken(idParamTVDB) && caps.supportsParam(SearchTypeTV, "tvdbid"):
		params.Set("tvdbid", tvdbID)
	case tvSearchSupported && cleanIMDB != "" && !c.idSearchBroken(idParamImdb) && caps.supportsParam(SearchTypeTV, "imdbid"):
		params.Set("imdbid", cleanIMDB)
	default:
		params.Set("q", queryFallback(imdbID, title))
		byID = false
	}
	if season > 0 && caps.supportsParam(SearchTypeTV, "season") {
		params.Set("season", strconv.Itoa(season))
	}
	if episode > 0 && caps.supportsParam(SearchTypeTV, "ep") {
		params.Set("ep", strconv.Itoa(episode))
	}

	cats := filterCategories(caps, c.resolveCategories(categories, []int{5000, 5010, 5030, 5040}))
	setCategories(params, cats)

	results, err := c.executeSearch(ctx, params, userAgent, byID)
	if err != nil {
		return nil, err
	}
	if len(results) > 0 || !byID {
		return results, nil
	}

	// Degradation ladder: each rung drops one more narrowing parameter. A
	// rung only applies when its last key is present in the original query,
	// otherwise it would duplicate the previous rung.
	for _, drop := range [][]string{{"ep"}, {"ep", "season"}, {"ep", "season", "cat"}} {
		if params.Get(drop[len(drop)-1]) == "" {
			continue
		}
		degraded := cloneWithout(params, drop...)
		retry, err := c.executeSearch(ctx, degraded, userAgent, byID)
		if err != nil {
			slog.DebugContext(ctx, "Newsnab degraded retry failed",
				"indexer", c.config.Name, "dropped", strings.Join(drop, "+"), "error", err)
			return results, nil
		}
		if len(retry) == 0 {
			continue
		}
		slog.DebugContext(ctx, "Newsnab search succeeded after dropping narrowing parameters",
			"indexer", c.config.Name, "dropped", strings.Join(drop, "+"), "results", len(retry))
		for i := range retry {
			retry[i].ByIDSearch = false
		}
		return retry, nil
	}

	identityParam := ""
	switch {
	case params.Get(idParamTVDB) != "":
		identityParam = idParamTVDB
	case params.Get(idParamImdb) != "":
		identityParam = idParamImdb
	}
	if identityParam == "" {
		return results, nil
	}

	// The bare identifier search returned nothing: this indexer likely
	// cannot resolve identifiers at all. Remember that for a while and
	// answer this request with a keyword search.
	c.markIDSearchBroken(identityParam)

	keyword := cloneWithout(params, identityParam, "ep", "season", "cat")
	keyword.Set("q", queryFallback(imdbID, title))
	keywordResults, err := c.executeSearch(ctx, keyword, userAgent, false)
	if err != nil {
		slog.DebugContext(ctx, "Newsnab keyword fallback after empty identifier search failed",
			"indexer", c.config.Name, "error", err)
		return results, nil
	}
	if len(keywordResults) > 0 {
		slog.DebugContext(ctx, "Newsnab identifier search unsupported; keyword fallback succeeded",
			"indexer", c.config.Name, "identifier", identityParam, "results", len(keywordResults))
	}
	return keywordResults, nil
}

// SearchGeneral performs a generic search by keyword query.
func (c *Client) SearchGeneral(ctx context.Context, query string, categories []int, userAgent string) ([]Result, error) {
	params := url.Values{}
	params.Set("t", "search")
	params.Set("q", sanitizeSearchQuery(query))

	caps := c.getCaps(ctx, userAgent)
	cats := filterCategories(caps, c.resolveCategories(categories, nil))
	setCategories(params, cats)

	return c.executeSearch(ctx, params, userAgent, false)
}

func (c *Client) resolveCategories(explicit []int, defaults []int) []int {
	if len(explicit) > 0 {
		return explicit
	}
	if len(c.config.Categories) > 0 {
		return c.config.Categories
	}
	return defaults
}

// Error is a structured Newznab API error payload
// (<error code=".." description=".."/> or its JSON equivalent).
type Error struct {
	Code        int    `json:"code"`
	Description string `json:"description"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("newznab API error %d: %s", e.Code, e.Description)
}

type apiErrorXML struct {
	Error struct {
		Code        string `xml:"code,attr"`
		Description string `xml:"description,attr"`
	} `xml:"error"`
}

// errorRootXML handles the standard Newznab shape where <error> is the
// document root element.
type errorRootXML struct {
	XMLName     xml.Name `xml:"error"`
	Code        string   `xml:"code,attr"`
	Description string   `xml:"description,attr"`
}

type apiErrorJSON struct {
	Error struct {
		Attributes struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"@attributes"`
	} `json:"error"`
}

// detectAPIError extracts a structured Newznab error payload if present.
// Successful RSS/JSON result payloads contain no error element and yield nil.
func detectAPIError(body []byte) error {
	var root errorRootXML
	if err := xml.Unmarshal(body, &root); err == nil && root.Code != "" {
		code, _ := strconv.Atoi(root.Code)
		return &Error{Code: code, Description: root.Description}
	}

	var x apiErrorXML
	if err := xml.Unmarshal(body, &x); err == nil && x.Error.Code != "" {
		code, _ := strconv.Atoi(x.Error.Code)
		return &Error{Code: code, Description: x.Error.Description}
	}

	var j apiErrorJSON
	if err := json.Unmarshal(body, &j); err == nil && j.Error.Attributes.Code != "" {
		code, _ := strconv.Atoi(j.Error.Attributes.Code)
		return &Error{Code: code, Description: j.Error.Attributes.Description}
	}

	return nil
}

// queryFallback picks the keyword used when an identifier parameter cannot be
// sent: the resolved title when known, otherwise the raw identifier.
func queryFallback(imdbID, title string) string {
	if title != "" {
		return sanitizeSearchQuery(title)
	}
	return imdbID
}

// sanitizeSearchQuery prepares a free-text title for a Newznab q= keyword
// search. Several backends (e.g. altHUB) return zero results when the query
// contains punctuation such as colons, even though the same releases are
// indexed under punctuation-free titles ("Re:ZERO" vs "Re Zero"). Replace the
// known-offending separators with spaces and collapse the result so scene
// titles keep their word boundaries.
func sanitizeSearchQuery(query string) string {
	if query == "" {
		return ""
	}
	replacer := strings.NewReplacer(":", " ", ",", " ", ";", " ")
	cleaned := replacer.Replace(query)
	return strings.Join(strings.Fields(cleaned), " ")
}

func setCategories(params url.Values, cats []int) {
	if len(cats) == 0 {
		return
	}
	catStrs := make([]string, len(cats))
	for i, v := range cats {
		catStrs[i] = strconv.Itoa(v)
	}
	params.Set("cat", strings.Join(catStrs, ","))
}

func stampByID(results []Result, byID bool) []Result {
	for i := range results {
		results[i].ByIDSearch = byID
	}
	return results
}

func (c *Client) executeSearch(ctx context.Context, params url.Values, userAgent string, byID bool) ([]Result, error) {
	params.Set("apikey", c.config.APIKey)
	params.Set("o", "json")
	params.Set("extended", "1")
	params.Set("limit", "100")

	baseURL := strings.TrimRight(c.config.URL, "/")
	reqURL := fmt.Sprintf("%s/api?%s", baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("newsnab: create request failed: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("newsnab: search request failed (%s): %w", c.config.Name, httpclient.RedactURLError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("newsnab: search returned HTTP %d (%s)", resp.StatusCode, c.config.Name)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("newsnab: read body failed: %w", err)
	}

	// Surface structured Newznab errors instead of silently returning zero results.
	if apiErr := detectAPIError(body); apiErr != nil {
		slog.WarnContext(ctx, "Newsnab indexer returned API error",
			"indexer", c.config.Name, "error", apiErr)
		return nil, apiErr
	}

	// First try parsing JSON
	results, err := c.parseJSONResults(body)
	if err == nil && len(results) > 0 {
		return stampByID(results, byID), nil
	}

	// Fallback to XML RSS parsing if indexer ignored o=json
	xmlResults, xmlErr := c.parseXMLResults(body)
	if xmlErr == nil {
		return stampByID(xmlResults, byID), nil
	}

	return stampByID(results, byID), nil
}

func (c *Client) parseJSONResults(body []byte) ([]Result, error) {
	var resp newsnabJSONResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	if len(resp.Channel.Item) == 0 {
		return []Result{}, nil
	}

	var singleItem newsnabJSONItem
	if err := json.Unmarshal(resp.Channel.Item, &singleItem); err == nil && singleItem.Title != "" {
		return []Result{c.convertJSONItem(singleItem)}, nil
	}

	var multiItems []newsnabJSONItem
	if err := json.Unmarshal(resp.Channel.Item, &multiItems); err == nil {
		out := make([]Result, 0, len(multiItems))
		for _, it := range multiItems {
			out = append(out, c.convertJSONItem(it))
		}
		return out, nil
	}

	return []Result{}, nil
}

// parseNewsnabTime parses publish dates from both XML and JSON Newznab feeds.
// RSS feeds use RFC 1123 timestamps, but several indexers emit ISO 8601 /
// RFC 3339 values in their JSON payloads, so both families are accepted.
func parseNewsnabTime(value string) time.Time {
	for _, layout := range []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (c *Client) convertJSONItem(it newsnabJSONItem) Result {
	downloadURL := it.Enclosure.Attributes.URL
	if downloadURL == "" {
		downloadURL = it.Enclosure.URL
	}
	if downloadURL == "" {
		downloadURL = it.Link
	}

	lengthStr := it.Enclosure.Attributes.Length
	if lengthStr == "" {
		lengthStr = it.Enclosure.Length
	}
	size, _ := strconv.ParseInt(lengthStr, 10, 64)

	var cat int
	var attrGuid string
	for _, attr := range it.Attr {
		name := attr.Attributes.Name
		if name == "" {
			name = attr.Name
		}
		val := attr.Attributes.Value
		if val == "" {
			val = attr.Value
		}

		if name == "size" && size == 0 {
			size, _ = strconv.ParseInt(val, 10, 64)
		}
		if name == "category" && cat == 0 {
			cat, _ = strconv.Atoi(val)
		}
		if name == "guid" && attrGuid == "" {
			attrGuid = val
		}
	}

	pubDate := parseNewsnabTime(it.PubDate)

	guid := attrGuid
	if guid == "" && len(it.GUID) > 0 {
		var guidStr string
		if err := json.Unmarshal(it.GUID, &guidStr); err == nil {
			guid = guidStr
		} else {
			var guidObj struct {
				Text  string `json:"#text"`
				Attrs struct {
					Text string `json:"text"`
				} `json:"@attributes"`
			}
			if err := json.Unmarshal(it.GUID, &guidObj); err == nil {
				if guidObj.Text != "" {
					guid = guidObj.Text
				} else {
					guid = guidObj.Attrs.Text
				}
			}
		}
	}
	if guid == "" {
		guid = it.Link
	}

	return Result{
		Title:       it.Title,
		DownloadURL: downloadURL,
		Size:        size,
		PublishDate: pubDate,
		Indexer:     c.config.Name,
		IndexerID:   c.config.ID,
		GUID:        guid,
		Category:    cat,
	}
}

func (c *Client) parseXMLResults(body []byte) ([]Result, error) {
	var rss newsnabXMLRSS
	if err := xml.Unmarshal(body, &rss); err != nil {
		return nil, err
	}

	out := make([]Result, 0, len(rss.Channel.Items))
	for _, it := range rss.Channel.Items {
		downloadURL := it.Enclosure.URL
		if downloadURL == "" {
			downloadURL = it.Link
		}
		size := it.Enclosure.Length
		var cat int
		for _, attr := range it.Attrs {
			if attr.Name == "size" && size == 0 {
				size, _ = strconv.ParseInt(attr.Value, 10, 64)
			}
			if attr.Name == "category" {
				cat, _ = strconv.Atoi(attr.Value)
			}
		}
		pubDate := parseNewsnabTime(it.PubDate)
		out = append(out, Result{
			Title:       it.Title,
			DownloadURL: downloadURL,
			Size:        size,
			PublishDate: pubDate,
			Indexer:     c.config.Name,
			IndexerID:   c.config.ID,
			GUID:        it.GUID.Value,
			Category:    cat,
		})
	}
	return out, nil
}
