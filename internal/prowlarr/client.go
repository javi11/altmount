package prowlarr

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	parsetorrentname "github.com/middelink/go-parse-torrent-name"
	"golift.io/starr"
	starrprowlarr "golift.io/starr/prowlarr"
)

// Client is a Prowlarr API client backed by golift/starr.
type Client struct {
	prowlarr *starrprowlarr.Prowlarr
	apiKey   string
	http     *http.Client
}

// NewClient creates a new Prowlarr client.
func NewClient(host, apiKey string) *Client {
	cfg := starr.New(apiKey, strings.TrimRight(host, "/"), 30*time.Second)
	return &Client{
		prowlarr: starrprowlarr.New(cfg),
		apiKey:   apiKey,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// NZBResult represents a single search result from Prowlarr.
type NZBResult struct {
	Title       string
	DownloadURL string
	Size        int64
	PublishDate time.Time
	Indexer     string
}

// matchesAnyKeyword returns true when title contains at least one of the
// keywords (case-insensitive). Returns true when keywords is empty (no filter).
func matchesAnyKeyword(title string, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	lower := strings.ToLower(title)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// MatchesLanguage returns true when title contains at least one of the language
// keywords (case-insensitive). Returns true when keywords is empty (no filter).
func MatchesLanguage(title string, keywords []string) bool {
	return matchesAnyKeyword(title, keywords)
}

// MatchesQuality returns true when title contains at least one of the quality
// keywords (case-insensitive). Returns true when keywords is empty (no filter).
func MatchesQuality(title string, keywords []string) bool {
	return matchesAnyKeyword(title, keywords)
}

// InferLanguage detects the most likely language from a release title using common scene/group
// naming conventions. Returns a short label like "Spanish", "French", or "" when not detected.
// PTN's language support is limited for European releases so we use custom keyword matching here.
func InferLanguage(title string) string {
	lower := strings.ToLower(title)

	type langRule struct {
		label    string
		keywords []string
	}
	rules := []langRule{
		{"Multi", []string{"multi", "multilingual", "multi.lang"}},
		{"Dual Audio", []string{"dual.audio", "dual audio", "dual-audio"}},
		{"Dual", []string{".dual.", " dual ", "-dual-"}},
		{"Spanish", []string{"spanish", ".esp.", " esp ", "-esp-", ".spa.", " spa ", "castellano", "cast.", " cast ", "🇪🇸"}},
		{"French", []string{"french", ".fre.", " fre ", ".vf.", " vf ", "vostfr", ".fr.", "🇫🇷"}},
		{"German", []string{"german", ".ger.", " ger ", ".de.", "deutsch", "🇩🇪"}},
		{"Italian", []string{"italian", ".ita.", " ita ", "🇮🇹"}},
		{"Portuguese", []string{"portuguese", ".por.", " por ", ".pt.", "pt-br", ".ptbr.", "🇧🇷", "🇵🇹"}},
		{"Japanese", []string{"japanese", ".jpn.", " jpn ", ".ja.", "🇯🇵"}},
		{"Korean", []string{"korean", ".kor.", " kor ", ".ko.", "🇰🇷"}},
		{"Chinese", []string{"chinese", ".chi.", " chi ", ".zh.", "🇨🇳", "🇹🇼"}},
		{"Russian", []string{"russian", ".rus.", " rus ", ".ru.", "🇷🇺"}},
		{"English", []string{"english", ".eng.", " eng "}},
	}
	for _, r := range rules {
		for _, kw := range r.keywords {
			if strings.Contains(lower, kw) {
				return r.label
			}
		}
	}
	return ""
}

// ReleaseMeta holds inferred metadata from a release title.
type ReleaseMeta struct {
	Language     string // "Spanish", "French", etc.
	FlagEmoji    string // "🇪🇸", "🇫🇷", etc. (empty for English/unknown)
	LangCode     string // "Esp", "Fra", "Deu", etc. (empty if unknown)
	QualityLabel string // "4K", "FHD", "HD", "SD", or quality string (e.g. "WEB-DL")
	Resolution   string // from PTN: "720p", "1080p", "2160p"
	Quality      string // from PTN: "WEB-DL", "BluRay", etc.
	Codec        string // from PTN: "x264", "x265", "HEVC", etc.
	Audio        string // from PTN: "AAC", "DTS", etc.
	ParsedTitle  string // PTN-parsed title (e.g. "La película")
	Year         int    // PTN-parsed year
}

var langFlags = map[string]string{
	"Spanish":    "🇪🇸",
	"French":     "🇫🇷",
	"German":     "🇩🇪",
	"Italian":    "🇮🇹",
	"Portuguese": "🇵🇹",
	"Japanese":   "🇯🇵",
	"Korean":     "🇰🇷",
	"Chinese":    "🇨🇳",
	"Russian":    "🇷🇺",
	"English":    "🇬🇧",
	"Multi":      "🌍",
	"Dual Audio": "🌍",
	"Dual":       "🌍",
}

var langCodes = map[string]string{
	"Spanish":    "Esp",
	"French":     "Fra",
	"German":     "Deu",
	"Italian":    "Ita",
	"Portuguese": "Por",
	"Japanese":   "Jpn",
	"Korean":     "Kor",
	"Chinese":    "Chi",
	"Russian":    "Rus",
	"English":    "Eng",
	"Multi":      "Multi",
	"Dual Audio": "Dual",
	"Dual":       "Dual",
}

func resolutionLabel(res string) string {
	switch res {
	case "2160p":
		return "4K"
	case "1080p":
		return "FHD"
	case "720p":
		return "HD"
	case "480p", "576p":
		return "SD"
	default:
		return ""
	}
}

// InferReleaseMeta parses a release title and returns detected metadata.
// Uses PTN for quality/resolution/codec/audio and custom logic for language.
func InferReleaseMeta(title string) ReleaseMeta {
	info, _ := parsetorrentname.Parse(title)
	meta := ReleaseMeta{
		Language: InferLanguage(title),
	}
	if info != nil {
		meta.Resolution = info.Resolution
		meta.Quality = info.Quality
		meta.Codec = info.Codec
		meta.Audio = info.Audio
		meta.ParsedTitle = info.Title
		meta.Year = info.Year
		if meta.Language == "" && info.Language != "" {
			meta.Language = info.Language
		}
	}
	meta.FlagEmoji = langFlags[meta.Language]
	meta.LangCode = langCodes[meta.Language]
	meta.QualityLabel = resolutionLabel(meta.Resolution)
	if meta.QualityLabel == "" {
		meta.QualityLabel = meta.Quality
	}
	return meta
}

// Search queries Prowlarr for NZB releases matching the given IMDB ID and categories.
// searchType should be "movie", "tvsearch", or "search".
// season and episode are optional (pass 0 to omit); used for tvsearch to scope results to a specific episode.
// Results are returned sorted by publish date descending (newest first).
func (c *Client) Search(ctx context.Context, imdbID, searchType string, categories []int, season, episode int) ([]NZBResult, error) {
	return c.searchWithID(ctx, "ImdbId", imdbID, searchType, categories, season, episode)
}

// SearchByTVDB queries Prowlarr for NZB releases using TVDB ID and categories.
// This is primarily used by TV series lookups when indexers support TvdbId but not ImdbId.
func (c *Client) SearchByTVDB(ctx context.Context, tvdbID, searchType string, categories []int, season, episode int) ([]NZBResult, error) {
	return c.searchWithID(ctx, "TvdbId", tvdbID, searchType, categories, season, episode)
}

func (c *Client) searchWithID(ctx context.Context, idField, idValue, searchType string, categories []int, season, episode int) ([]NZBResult, error) {
	var query strings.Builder
	if idValue != "" {
		query.WriteString("{" + idField + ":" + idValue + "}")
	}
	if season > 0 {
		query.WriteString("{Season:" + strconv.Itoa(season) + "}")
	}
	if episode > 0 {
		query.WriteString("{Episode:" + strconv.Itoa(episode) + "}")
	}

	cats := make([]int64, len(categories))
	for i, cat := range categories {
		cats[i] = int64(cat)
	}

	input := starrprowlarr.SearchInput{
		Query:      query.String(),
		Type:       searchType,
		Categories: cats,
	}

	releases, err := c.prowlarr.SearchContext(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: search failed: %w", err)
	}

	results := make([]NZBResult, 0, len(releases))
	for _, r := range releases {
		if r.DownloadURL == "" || r.Protocol != "usenet" {
			continue
		}
		results = append(results, NZBResult{
			Title:       r.Title,
			DownloadURL: r.DownloadURL,
			Size:        r.Size,
			PublishDate: r.PublishDate,
			Indexer:     r.Indexer,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].PublishDate.After(results[j].PublishDate)
	})

	return results, nil
}

// DownloadNZB fetches the NZB file content from the given Prowlarr download URL.
func (c *Client) DownloadNZB(ctx context.Context, downloadURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: create download request: %w", err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("prowlarr: download returned status %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}
