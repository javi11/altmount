package stremio

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/newsnab"
	"github.com/javi11/altmount/internal/prowlarr"
)

// SearchParams contains the query parameters for a Stremio stream search.
type SearchParams struct {
	Type          string // "movie" or "series"
	IMDBID        string // "tt1234567"
	Title         string // "Arcane"
	Season        int    // 1
	Episode       int    // 1
	TVDBID        string // optional
	TimeoutMS     int    // e.g. 3500
	CustomUA      string // optional
}

// CoordinatorConfig defines the search provider configuration.
type CoordinatorConfig struct {
	Provider        string                 `yaml:"provider" mapstructure:"provider" json:"provider"` // "prowlarr", "newsnab", "both"
	UserAgentMode   string                 `yaml:"user_agent_mode" mapstructure:"user_agent_mode" json:"user_agent_mode"` // "auto", "custom"
	CustomUserAgent string                 `yaml:"custom_user_agent" mapstructure:"custom_user_agent" json:"custom_user_agent"`
	ProwlarrHost    string                 `yaml:"prowlarr_host" mapstructure:"prowlarr_host" json:"prowlarr_host"`
	ProwlarrKey     string                 `yaml:"prowlarr_key" mapstructure:"prowlarr_key" json:"prowlarr_key"`
	ProwlarrCats    []int                  `yaml:"prowlarr_categories" mapstructure:"prowlarr_categories" json:"prowlarr_categories"`
	ProwlarrIdxs    []int                  `yaml:"prowlarr_indexers" mapstructure:"prowlarr_indexers" json:"prowlarr_indexers"`
	NewsnabIndexers []newsnab.IndexerConfig `yaml:"newsnab_indexers" mapstructure:"newsnab_indexers" json:"newsnab_indexers"`
	Scoring         StreamScoringConfig    `yaml:"scoring" mapstructure:"scoring" json:"scoring"`
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

// Search executes concurrent queries across all enabled providers, deduplicates and ranks results.
func (sc *SearchCoordinator) Search(ctx context.Context, params SearchParams) ([]ScoredRelease, error) {
	timeout := time.Duration(params.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 3500 * time.Millisecond
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
		wg            sync.WaitGroup
		mu            sync.Mutex
		aggregated    []SearchResult
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
				pResults, err = sc.prowlarrClient.Search(searchCtx, params.IMDBID, "movie", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, 0, 0)
			} else {
				if params.TVDBID != "" {
					pResults, err = sc.prowlarrClient.SearchByTVDB(searchCtx, params.TVDBID, "tvsearch", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, params.Season, params.Episode)
				}
				if len(pResults) == 0 && params.IMDBID != "" {
					pResults, err = sc.prowlarrClient.Search(searchCtx, params.IMDBID, "tvsearch", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, params.Season, params.Episode)
				}
				if len(pResults) == 0 && params.Title != "" {
					pResults, err = sc.prowlarrClient.Search(searchCtx, params.Title, "tvsearch", sc.config.ProwlarrCats, sc.config.ProwlarrIdxs, params.Season, params.Episode)
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
					nResults, err = c.SearchMovie(searchCtx, params.IMDBID, nil, userAgent)
				} else {
					nResults, err = c.SearchTV(searchCtx, params.IMDBID, params.TVDBID, params.Title, params.Season, params.Episode, nil, userAgent)
					// Fallback to title query if ID search yielded no results
					if (err == nil && len(nResults) == 0) && params.Title != "" && (params.TVDBID != "" || params.IMDBID != "") {
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
						})
					}
					mu.Unlock()
				}
			}()
		}
	}

	wg.Wait()

	// Deduplicate aggregated items by DownloadURL / Title and validate against requested media
	uniqueResults := make([]SearchResult, 0, len(aggregated))
	seenURLs := make(map[string]struct{})
	seenTitles := make(map[string]struct{})

	for _, res := range aggregated {
		// Validate series / movie release matches the requested media (1:1 Sonarr/Radarr/Prowlarr release validation)
		if strings.EqualFold(params.Type, "series") {
			if !MatchesSeries(res.Title, params.Title, params.Season, params.Episode, 0) {
				continue
			}
		} else if strings.EqualFold(params.Type, "movie") {
			if !MatchesMovie(res.Title, params.Title, 0) {
				continue
			}
		}

		if res.DownloadURL != "" {
			if _, exists := seenURLs[res.DownloadURL]; exists {
				continue
			}
			seenURLs[res.DownloadURL] = struct{}{}
		}
		titleLower := strings.ToLower(res.Title)
		if _, exists := seenTitles[titleLower]; exists {
			continue
		}
		seenTitles[titleLower] = struct{}{}

		uniqueResults = append(uniqueResults, res)
	}

	// Rank, score, and filter results
	scored := RankAndFilterReleases(uniqueResults, &sc.config.Scoring, indexerWeights)
	return scored, nil
}
