package stremio

import (
	"context"
	"fmt"
	"net/http"
	"sort"
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

	// 1. Dispatch Prowlarr Search
	if (provider == "prowlarr" || provider == "both") && sc.prowlarrClient != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var pResults []prowlarr.NZBResult
			var err error

			if strings.EqualFold(params.Type, "movie") {
				if params.IMDBID != "" {
					pResults, err = sc.prowlarrClient.Search(searchCtx, params.IMDBID, "movie", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, 0, 0)
				}
				if len(pResults) == 0 && params.Title != "" {
					pResults, err = sc.prowlarrClient.SearchByQuery(searchCtx, params.Title, "movie", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, 0, 0)
				}
			} else {
				if params.TVDBID != "" {
					pResults, err = sc.prowlarrClient.SearchByTVDB(searchCtx, params.TVDBID, "tvsearch", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, params.Season, params.Episode)
				}
				if len(pResults) == 0 && params.IMDBID != "" {
					pResults, err = sc.prowlarrClient.Search(searchCtx, params.IMDBID, "tvsearch", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, params.Season, params.Episode)
				}
				if len(pResults) == 0 && params.Title != "" {
					pResults, err = sc.prowlarrClient.SearchByQuery(searchCtx, params.Title, "tvsearch", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, params.Season, params.Episode)
				}
			}

			if err == nil && len(pResults) > 0 {
				mu.Lock()
				for _, r := range pResults {
					aggregated = append(aggregated, SearchResult{
						Title:       r.Title,
						DownloadURL: r.DownloadURL,
						Size:        r.Size,
						PublishDate: r.PublishDate,
						Indexer:     r.Indexer,
						IndexerID:   fmt.Sprintf("%d", r.IndexerID),
						Source:      "prowlarr",
						GUID:        r.GUID,
					})
				}
				mu.Unlock()
			}
		}()
	}

	// 2. Dispatch Direct Newsnab Searches
	if (provider == "newsnab" || provider == "both") && len(sc.newsnabClients) > 0 {
		for _, client := range sc.newsnabClients {
			c := client
			wg.Add(1)
			go func() {
				defer wg.Done()
				var nResults []newsnab.Result
				var err error

				if strings.EqualFold(params.Type, "movie") {
					if params.IMDBID != "" {
						nResults, err = c.SearchMovie(searchCtx, params.IMDBID, nil, userAgent)
					}
					if (err == nil && len(nResults) == 0) && params.Title != "" {
						nResults, err = c.SearchGeneral(searchCtx, params.Title, []int{2000, 2010, 2030, 2040, 2045, 2060}, userAgent)
					}
				} else {
					nResults, err = c.SearchTV(searchCtx, params.IMDBID, params.TVDBID, params.Title, params.Season, params.Episode, nil, userAgent)
					// Fallback to title query if ID search yielded no results
					if (err == nil && len(nResults) == 0) && params.Title != "" {
						if fallbackResults, fbErr := c.SearchTV(searchCtx, "", "", params.Title, params.Season, params.Episode, nil, userAgent); fbErr == nil && len(fallbackResults) > 0 {
							nResults = append(nResults, fallbackResults...)
						}
					}
				}

				if err == nil && len(nResults) > 0 {
					mu.Lock()
					for _, r := range nResults {
						aggregated = append(aggregated, SearchResult{
							Title:       r.Title,
							DownloadURL: r.DownloadURL,
							Size:        r.Size,
							PublishDate: r.PublishDate,
							Indexer:     r.Indexer,
							IndexerID:   r.IndexerID,
							GUID:        r.GUID,
							Source:      "newsnab",
						})
					}
					mu.Unlock()
				}
			}()
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
		// Evaluate media matching if title is provided
		isMediaMismatch := false
		var mismatchReason string
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
			if bonus, ok := indexerWeights[rel.Indexer]; ok && bonus != 0 {
				eval.Score += bonus
			} else if bonus, ok := indexerWeights[rel.IndexerID]; ok && bonus != 0 {
				eval.Score += bonus
			}
			activeList = append(activeList, eval)
		}
	}

	// Sort active releases descending by score, ties broken by newest date
	sort.Slice(activeList, func(i, j int) bool {
		if activeList[i].Score != activeList[j].Score {
			return activeList[i].Score > activeList[j].Score
		}
		return activeList[i].PublishDate.After(activeList[j].PublishDate)
	})

	// Sort discarded releases descending by date
	sort.Slice(discardedList, func(i, j int) bool {
		return discardedList[i].PublishDate.After(discardedList[j].PublishDate)
	})

	allEvaluated := append(activeList, discardedList...)

	return &SearchInspectResult{
		TotalResults:     len(allEvaluated),
		ActiveResults:    len(activeList),
		DiscardedResults: len(discardedList),
		Releases:         allEvaluated,
	}, nil
}
