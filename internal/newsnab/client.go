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
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return fmt.Errorf("newsnab: download redirect is not allowed")
	}
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
// support, mirroring Prowlarr/Radarr per-indexer behavior.
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
	case caps.supportsParam("imdbid"):
		params.Set("t", "movie")
		params.Set("imdbid", cleanIMDB)
	default:
		params.Set("t", "movie")
		params.Set("q", queryFallback(imdbID, title))
		byID = false
	}

	cats := filterCategories(caps, c.resolveCategories(categories, []int{2000, 2010, 2030, 2040, 2045, 2060}))
	setCategories(params, cats)

	return c.executeSearch(ctx, params, userAgent, byID)
}

// SearchTV searches for TV episode releases by IMDB ID, TVDB ID, title, season, and episode.
// Follows Prowlarr/Sonarr parameter prioritization, skipping parameters the
// indexer does not advertise via caps:
// Priority 1: tvdbid
// Priority 2: imdbid
// Priority 3: q=title (fallback text query)
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
	case tvSearchSupported && tvdbID != "" && caps.supportsParam("tvdbid"):
		params.Set("tvdbid", tvdbID)
	case tvSearchSupported && cleanIMDB != "" && caps.supportsParam("imdbid"):
		params.Set("imdbid", cleanIMDB)
	default:
		params.Set("q", queryFallback(imdbID, title))
		byID = false
	}
	if season > 0 && caps.supportsParam("season") {
		params.Set("season", strconv.Itoa(season))
	}
	if episode > 0 && caps.supportsParam("ep") {
		params.Set("ep", strconv.Itoa(episode))
	}

	cats := filterCategories(caps, c.resolveCategories(categories, []int{5000, 5010, 5030, 5040}))
	setCategories(params, cats)

	return c.executeSearch(ctx, params, userAgent, byID)
}

// SearchGeneral performs a generic search by keyword query.
func (c *Client) SearchGeneral(ctx context.Context, query string, categories []int, userAgent string) ([]Result, error) {
	params := url.Values{}
	params.Set("t", "search")
	params.Set("q", query)

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
		return title
	}
	return imdbID
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
