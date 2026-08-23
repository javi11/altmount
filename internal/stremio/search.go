package stremio

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/newsnab"
	"github.com/javi11/altmount/internal/prowlarr"
)

// SearchParams contains the query parameters for a Stremio stream search.
type SearchParams struct {
	Type      string // "movie" or "series"
	IMDBID    string // "tt1234567"
	Title     string // "Arcane"
	Season    int    // 1
	Episode   int    // 1
	TVDBID    string // optional
	TimeoutMS int    // e.g. 3500
	CustomUA  string // optional
}

// CoordinatorConfig defines the search provider configuration.
type CoordinatorConfig struct {
	Provider        string                  `yaml:"provider" mapstructure:"provider" json:"provider"`                      // "prowlarr", "newsnab", "both"
	UserAgentMode   string                  `yaml:"user_agent_mode" mapstructure:"user_agent_mode" json:"user_agent_mode"` // "auto", "custom"
	CustomUserAgent string                  `yaml:"custom_user_agent" mapstructure:"custom_user_agent" json:"custom_user_agent"`
	ProwlarrHost    string                  `yaml:"prowlarr_host" mapstructure:"prowlarr_host" json:"prowlarr_host"`
	ProwlarrKey     string                  `yaml:"prowlarr_key" mapstructure:"prowlarr_key" json:"prowlarr_key"`
	ProwlarrCats    []int                   `yaml:"prowlarr_categories" mapstructure:"prowlarr_categories" json:"prowlarr_categories"`
	ProwlarrIdxs    []int                   `yaml:"prowlarr_indexers" mapstructure:"prowlarr_indexers" json:"prowlarr_indexers"`
	NewsnabIndexers []newsnab.IndexerConfig `yaml:"newsnab_indexers" mapstructure:"newsnab_indexers" json:"newsnab_indexers"`
	Scoring         StreamScoringConfig     `yaml:"scoring" mapstructure:"scoring" json:"scoring"`
}

// SearchCoordinator coordinates multi-provider indexer searches and ranking.
type SearchCoordinator struct {
	prowlarrClient *prowlarr.Client
	newsnabClients []*newsnab.Client
	uaManager      *UserAgentManager
	config         CoordinatorConfig
	httpClient     *http.Client
}

// NewSearchCoordinator creates a new search coordinator.
func NewSearchCoordinator(cfg CoordinatorConfig, httpClient *http.Client) *SearchCoordinator {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	var prowlarrClient *prowlarr.Client
	if cfg.ProwlarrHost != "" && cfg.ProwlarrKey != "" {
		prowlarrClient = prowlarr.NewClient(cfg.ProwlarrHost, cfg.ProwlarrKey, httpClient)
	}

	newsnabClients := make([]*newsnab.Client, 0, len(cfg.NewsnabIndexers))
	for _, nCfg := range cfg.NewsnabIndexers {
		if nCfg.Enabled && nCfg.URL != "" {
			newsnabClients = append(newsnabClients, newsnab.NewClient(nCfg, httpClient))
		}
	}

	return &SearchCoordinator{
		prowlarrClient: prowlarrClient,
		newsnabClients: newsnabClients,
		uaManager:      GetUserAgentManager(),
		config:         cfg,
		httpClient:     httpClient,
	}
}

// SearchInspectResult contains evaluation diagnostics for all releases found during an indexer query.
type SearchInspectResult struct {
	TotalResults     int             `json:"total_results"`
	ActiveResults    int             `json:"active_results"`
	DiscardedResults int             `json:"discarded_results"`
	Releases         []ScoredRelease `json:"releases"`
}

// Search executes concurrent queries across all enabled providers, deduplicates and ranks results.
func (sc *SearchCoordinator) Search(ctx context.Context, params SearchParams) ([]ScoredRelease, error) {
	inspect, err := sc.SearchInspect(ctx, params)
	if err != nil {
		return nil, err
	}

	// Filter down to active (non-excluded) releases only
	active := make([]ScoredRelease, 0, inspect.ActiveResults)
	for _, rel := range inspect.Releases {
		if !rel.Excluded {
			active = append(active, rel)
		}
	}
	return active, nil
}

// SearchInspect executes concurrent queries across all enabled providers, evaluates every release against
// scoring/exclusion rules, and returns both active and discarded releases with diagnostic reasons.
func (sc *SearchCoordinator) SearchInspect(ctx context.Context, params SearchParams) (*SearchInspectResult, error) {
	timeout := time.Duration(params.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5000 * time.Millisecond
	}

	searchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	userAgent := sc.uaManager.GetUserAgent(params.Type, sc.config.CustomUserAgent)
	if params.CustomUA != "" {
		userAgent = params.CustomUA
	}

	provider := strings.ToLower(sc.config.Provider)
	if provider == "" {
		provider = "both"
	}

	slog.InfoContext(searchCtx, "SearchInspect starting", "provider", provider, "has_prowlarr", sc.prowlarrClient != nil, "newsnab_count", len(sc.newsnabClients), "params_type", params.Type, "params_title", params.Title, "params_imdb", params.IMDBID)

	var (
		wg             sync.WaitGroup
		mu             sync.Mutex
		aggregated     []SearchResult
		indexerWeights = make(map[string]int)
	)

	// Populate weights from newsnab indexers
	for _, n := range sc.config.NewsnabIndexers {
		if n.Weight != 0 {
			indexerWeights[n.Name] = n.Weight
			indexerWeights[n.ID] = n.Weight
		}
	}

	// 1. Dispatch Prowlarr Searches concurrently
	if (provider == "prowlarr" || provider == "both") && sc.prowlarrClient != nil {
		if strings.EqualFold(params.Type, "movie") {
			if params.IMDBID != "" {
				wg.Add(1)
				go func() {
					defer wg.Done()
					res, err := sc.prowlarrClient.Search(searchCtx, params.IMDBID, "movie", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, 0, 0)
					if err != nil {
						slog.WarnContext(searchCtx, "Prowlarr movie ID search failed", "error", err, "imdb_id", params.IMDBID)
					} else {
						slog.InfoContext(searchCtx, "Prowlarr movie ID search returned results", "count", len(res), "imdb_id", params.IMDBID)
					}
					if err == nil && len(res) > 0 {
						mu.Lock()
						for _, r := range res {
							aggregated = append(aggregated, prowlarrToSearchResult(r, true))
						}
						mu.Unlock()
					}
				}()
			}
			if params.Title != "" {
				wg.Add(1)
				go func() {
					defer wg.Done()
					res, err := sc.prowlarrClient.SearchByQuery(searchCtx, params.Title, "movie", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, 0, 0)
					if err != nil {
						slog.WarnContext(searchCtx, "Prowlarr movie Title search failed", "error", err, "title", params.Title)
					} else {
						slog.InfoContext(searchCtx, "Prowlarr movie Title search returned results", "count", len(res), "title", params.Title)
					}
					if err == nil && len(res) > 0 {
						mu.Lock()
						for _, r := range res {
							aggregated = append(aggregated, prowlarrToSearchResult(r, false))
						}
						mu.Unlock()
					}
				}()
			}
		} else {
			if params.TVDBID != "" {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if res, err := sc.prowlarrClient.SearchByTVDB(searchCtx, params.TVDBID, "tvsearch", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, params.Season, params.Episode); err == nil && len(res) > 0 {
						mu.Lock()
						for _, r := range res {
							aggregated = append(aggregated, prowlarrToSearchResult(r, true))
						}
						mu.Unlock()
					}
				}()
			}
			if params.IMDBID != "" {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if res, err := sc.prowlarrClient.Search(searchCtx, params.IMDBID, "tvsearch", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, params.Season, params.Episode); err == nil && len(res) > 0 {
						mu.Lock()
						for _, r := range res {
							aggregated = append(aggregated, prowlarrToSearchResult(r, true))
						}
						mu.Unlock()
					}
				}()
			}
			if params.Title != "" {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if res, err := sc.prowlarrClient.SearchByQuery(searchCtx, params.Title, "tvsearch", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, params.Season, params.Episode); err == nil && len(res) > 0 {
						mu.Lock()
						for _, r := range res {
							aggregated = append(aggregated, prowlarrToSearchResult(r, false))
						}
						mu.Unlock()
					}
				}()
			}
		}
	}

	// 2. Dispatch Direct Newsnab Searches concurrently.
	// Mirrors Prowlarr/Sonarr/Radarr: identifier query first, keyword query
	// only as a sequential fallback when the ID query is unsupported, errors,
	// or returns nothing. This halves indexer API usage vs firing both always.
	if (provider == "newsnab" || provider == "both") && len(sc.newsnabClients) > 0 {
		for _, client := range sc.newsnabClients {
			c := client
			if strings.EqualFold(params.Type, "movie") {
				if params.IMDBID != "" {
					wg.Add(1)
					go func() {
						defer wg.Done()
						res, err := c.SearchMovie(searchCtx, params.IMDBID, params.Title, nil, userAgent)
						if err != nil {
							slog.WarnContext(searchCtx, "Newsnab movie ID search failed", "indexer", c.Name(), "error", err, "imdb_id", params.IMDBID)
						} else {
							slog.InfoContext(searchCtx, "Newsnab movie ID search returned results", "indexer", c.Name(), "count", len(res), "imdb_id", params.IMDBID)
						}
						if (err != nil || len(res) == 0) && params.Title != "" {
							fallbackRes, ferr := c.SearchGeneral(searchCtx, params.Title, []int{2000, 2010, 2030, 2040, 2045, 2060}, userAgent)
							if ferr != nil {
								slog.WarnContext(searchCtx, "Newsnab movie keyword fallback failed", "indexer", c.Name(), "error", ferr, "title", params.Title)
							} else {
								slog.InfoContext(searchCtx, "Newsnab movie keyword fallback returned results", "indexer", c.Name(), "count", len(fallbackRes), "title", params.Title)
							}
							if len(fallbackRes) > 0 {
								res = fallbackRes
							}
						}
						if len(res) > 0 {
							mu.Lock()
							for _, r := range res {
								aggregated = append(aggregated, newsnabToSearchResult(r))
							}
							mu.Unlock()
						}
					}()
				} else if params.Title != "" {
					wg.Add(1)
					go func() {
						defer wg.Done()
						res, err := c.SearchGeneral(searchCtx, params.Title, []int{2000, 2010, 2030, 2040, 2045, 2060}, userAgent)
						if err != nil {
							slog.WarnContext(searchCtx, "Newsnab movie Title search failed", "indexer", c.Name(), "error", err, "title", params.Title)
						} else {
							slog.InfoContext(searchCtx, "Newsnab movie Title search returned results", "indexer", c.Name(), "count", len(res), "title", params.Title)
						}
						if err == nil && len(res) > 0 {
							mu.Lock()
							for _, r := range res {
								aggregated = append(aggregated, newsnabToSearchResult(r))
							}
							mu.Unlock()
						}
					}()
				}
			} else {
				if params.IMDBID != "" || params.TVDBID != "" {
					wg.Add(1)
					go func() {
						defer wg.Done()
						res, err := c.SearchTV(searchCtx, params.IMDBID, params.TVDBID, "", params.Season, params.Episode, nil, userAgent)
						if err != nil {
							slog.WarnContext(searchCtx, "Newsnab TV ID search failed", "indexer", c.Name(), "error", err)
						} else {
							slog.InfoContext(searchCtx, "Newsnab TV ID search returned results", "indexer", c.Name(), "count", len(res))
						}
						if (err != nil || len(res) == 0) && params.Title != "" {
							fallbackRes, ferr := c.SearchTV(searchCtx, "", "", params.Title, params.Season, params.Episode, nil, userAgent)
							if ferr != nil {
								slog.WarnContext(searchCtx, "Newsnab TV keyword fallback failed", "indexer", c.Name(), "error", ferr, "title", params.Title)
							} else {
								slog.InfoContext(searchCtx, "Newsnab TV keyword fallback returned results", "indexer", c.Name(), "count", len(fallbackRes), "title", params.Title)
							}
							if len(fallbackRes) > 0 {
								res = fallbackRes
							}
						}
						if len(res) > 0 {
							mu.Lock()
							for _, r := range res {
								aggregated = append(aggregated, newsnabToSearchResult(r))
							}
							mu.Unlock()
						}
					}()
				} else if params.Title != "" {
					wg.Add(1)
					go func() {
						defer wg.Done()
						res, err := c.SearchTV(searchCtx, "", "", params.Title, params.Season, params.Episode, nil, userAgent)
						if err != nil {
							slog.WarnContext(searchCtx, "Newsnab TV Title search failed", "indexer", c.Name(), "error", err, "title", params.Title)
						} else {
							slog.InfoContext(searchCtx, "Newsnab TV Title search returned results", "indexer", c.Name(), "count", len(res), "title", params.Title)
						}
						if err == nil && len(res) > 0 {
							mu.Lock()
							for _, r := range res {
								aggregated = append(aggregated, newsnabToSearchResult(r))
							}
							mu.Unlock()
						}
					}()
				}
			}
		}
	}

	wg.Wait()

	// Deduplicate aggregated items by DownloadURL / Title identity
	uniqueResults := make([]SearchResult, 0, len(aggregated))
	seenURLs := make(map[string]struct{})
	seenReleases := make(map[string]struct{})

	for _, res := range aggregated {
		if res.DownloadURL != "" {
			if _, exists := seenURLs[res.DownloadURL]; exists {
				continue
			}
			seenURLs[res.DownloadURL] = struct{}{}
		}
		identity := res.Source + "|" + res.IndexerID + "|" + res.GUID + "|" + res.DownloadURL
		if _, exists := seenReleases[identity]; exists {
			continue
		}
		seenReleases[identity] = struct{}{}

		uniqueResults = append(uniqueResults, res)
	}

	activeList := make([]ScoredRelease, 0, len(uniqueResults))
	discardedList := make([]ScoredRelease, 0, len(uniqueResults))

	for _, rel := range uniqueResults {
		// Evaluate media matching if title is provided. Releases matched by
		// the indexer via an identifier query (imdbid/tvdbid) skip the gate:
		// the indexer resolved the ID itself, so release names that differ
		// from the metadata title (foreign/alternative titles) are still
		// valid — same trust model as Prowlarr/Sonarr/Radarr.
		isMediaMismatch := false
		var mismatchReason string
		if !rel.ByIDSearch {
			if strings.EqualFold(params.Type, "series") && params.Title != "" {
				if !MatchesSeries(rel.Title, params.Title, params.Season, params.Episode, 0) {
					isMediaMismatch = true
					mismatchReason = "Does not match requested series or episode"
				}
			} else if strings.EqualFold(params.Type, "movie") && params.Title != "" {
				if !MatchesMovie(rel.Title, params.Title, 0) {
					isMediaMismatch = true
					mismatchReason = "Does not match requested movie title"
				}
			}
		}

		eval := EvaluateRelease(rel.Title, &sc.config.Scoring)
		eval.SearchResult = rel

		if isMediaMismatch {
			eval.Excluded = true
			if eval.ExcludeReason != "" {
				eval.ExcludeReason = mismatchReason + "; " + eval.ExcludeReason
			} else {
				eval.ExcludeReason = mismatchReason
			}
		}

		if eval.Excluded {
			discardedList = append(discardedList, eval)
		} else {
			// Apply indexer bonus weights to active releases
			applyIndexerBonus(&eval, rel, indexerWeights)
			activeList = append(activeList, eval)
		}
	}

	sortByScoreDesc(activeList)
	sortByDateDesc(discardedList)

	allEvaluated := append(activeList, discardedList...)

	return &SearchInspectResult{
		TotalResults:     len(allEvaluated),
		ActiveResults:    len(activeList),
		DiscardedResults: len(discardedList),
		Releases:         allEvaluated,
	}, nil
}

func prowlarrToSearchResult(r prowlarr.NZBResult, byID bool) SearchResult {
	return SearchResult{
		Title:       r.Title,
		DownloadURL: r.DownloadURL,
		Size:        r.Size,
		PublishDate: r.PublishDate,
		Indexer:     r.Indexer,
		IndexerID:   fmt.Sprintf("%d", r.IndexerID),
		Source:      "prowlarr",
		GUID:        r.GUID,
		ByIDSearch:  byID,
	}
}

func newsnabToSearchResult(r newsnab.Result) SearchResult {
	return SearchResult{
		Title:       r.Title,
		DownloadURL: r.DownloadURL,
		Size:        r.Size,
		PublishDate: r.PublishDate,
		Indexer:     r.Indexer,
		IndexerID:   r.IndexerID,
		GUID:        r.GUID,
		Source:      "newsnab",
		ByIDSearch:  r.ByIDSearch,
	}
}
