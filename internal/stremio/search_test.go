package stremio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/javi11/altmount/internal/newsnab"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCapsXML advertises every search type and parameter so coordinator tests
// exercise the standard query shapes.
const testCapsXML = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<server title="Mock Indexer"/>
	<searching>
		<search available="yes" supportedParams="q,cat,limit,extended"/>
		<movie-search available="yes" supportedParams="q,cat,imdbid,limit,extended"/>
		<tv-search available="yes" supportedParams="q,cat,imdbid,tvdbid,season,ep,limit,extended"/>
	</searching>
</caps>`

func TestSearchTV_NewznabQueryPriority_Prowlarr1to1(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(testCapsXML))
			return
		}
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		// A non-empty result keeps the degradation ladder out of the picture:
		// this test pins query-shape priority, not empty-result behavior.
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>test</title><item><title>The Ark S01E01</title><enclosure url="http://idx/the-ark.nzb" type="application/x-nzb"/></item></channel></rss>`))
	}))
	defer server.Close()

	client := newsnab.NewClient(newsnab.IndexerConfig{
		Name: "test-indexer",
		URL:  server.URL,
		APIKey: "dummy-key",
	}, server.Client())

	ctx := context.Background()

	t.Run("Prioritizes TVDB ID when available (omits q)", func(t *testing.T) {
		_, err := client.SearchTV(ctx, "tt15367376", "415089", "The Ark", 1, 1, nil, "altmount-test")
		require.NoError(t, err)
		assert.Contains(t, capturedQuery, "tvdbid=415089")
		assert.Contains(t, capturedQuery, "season=1")
		assert.Contains(t, capturedQuery, "ep=1")
		assert.NotContains(t, capturedQuery, "q=")
	})

	t.Run("Prioritizes IMDb ID when TVDB ID is empty (omits q)", func(t *testing.T) {
		_, err := client.SearchTV(ctx, "tt15367376", "", "The Ark", 1, 1, nil, "altmount-test")
		require.NoError(t, err)
		assert.Contains(t, capturedQuery, "imdbid=15367376")
		assert.Contains(t, capturedQuery, "season=1")
		assert.Contains(t, capturedQuery, "ep=1")
		assert.NotContains(t, capturedQuery, "q=")
	})

	t.Run("Falls back to q=title when no IDs are available", func(t *testing.T) {
		_, err := client.SearchTV(ctx, "", "", "The Ark", 1, 1, nil, "altmount-test")
		require.NoError(t, err)
		assert.Contains(t, capturedQuery, "q=The+Ark")
		assert.Contains(t, capturedQuery, "season=1")
		assert.Contains(t, capturedQuery, "ep=1")
		assert.NotContains(t, capturedQuery, "tvdbid=")
		assert.NotContains(t, capturedQuery, "imdbid=")
	})
}

func TestSearch_TheArk_vs_ArkTheAnimatedSeries_Filtering(t *testing.T) {
	// Sample indexer response returning mixed search results for "The Ark" S01E01
	sampleFeedXML := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
  <channel>
    <title>Mock Indexer</title>
    <item>
      <title>The.Ark.S01E01.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb</title>
      <link>https://indexer.example/dl/1.nzb</link>
      <enclosure url="https://indexer.example/dl/1.nzb" length="2500000000" type="application/x-nzb" />
    </item>
    <item>
      <title>The.Ark.2023.S01E01.2160p.WEB-DL.DDP5.1.HDR.H.265-FLUX</title>
      <link>https://indexer.example/dl/2.nzb</link>
      <enclosure url="https://indexer.example/dl/2.nzb" length="5500000000" type="application/x-nzb" />
    </item>
    <item>
      <title>ARK.The.Animated.Series.S01E01.Element.1.2160p.AMZN.WEB-DL.DDP5.1.HEVC-NTb</title>
      <link>https://indexer.example/dl/3.nzb</link>
      <enclosure url="https://indexer.example/dl/3.nzb" length="5586927832" type="application/x-nzb" />
    </item>
    <item>
      <title>ARK - The Animated Series (2024) S01E01 (2160p AMZN WEB-DL H265 SDR DDP 5.1 English - HONE)</title>
      <link>https://indexer.example/dl/4.nzb</link>
      <enclosure url="https://indexer.example/dl/4.nzb" length="5586928505" type="application/x-nzb" />
    </item>
    <item>
      <title>Norway.The.Dark.Horse.S01E01.HDR.2160p.WEB.h265-EDITH</title>
      <link>https://indexer.example/dl/5.nzb</link>
      <enclosure url="https://indexer.example/dl/5.nzb" length="9382540943" type="application/x-nzb" />
    </item>
    <item>
      <title>The.Ark.S01E02.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb</title>
      <link>https://indexer.example/dl/6.nzb</link>
      <enclosure url="https://indexer.example/dl/6.nzb" length="2500000000" type="application/x-nzb" />
    </item>
  </channel>
</rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(testCapsXML))
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleFeedXML))
	}))
	defer server.Close()

	coord := NewSearchCoordinator(CoordinatorConfig{
		Provider: "newsnab",
		NewsnabIndexers: []newsnab.IndexerConfig{
			{
				Name:    "mock-indexer",
				URL:     server.URL,
				APIKey:  "dummy-key",
				Enabled: true,
			},
		},
	}, server.Client())

	ctx := context.Background()

	t.Run("Keyword search strictly retains The Ark and rejects ARK The Animated Series", func(t *testing.T) {
		results, err := coord.Search(ctx, SearchParams{
			Type:      "series",
			Title:     "The Ark",
			Season:    1,
			Episode:   1,
			TimeoutMS: 2000,
		})
		require.NoError(t, err)

		// Expect only the two genuine The Ark S01E01 releases
		require.Len(t, results, 2)
		for _, r := range results {
			assert.Contains(t, r.Title, "The.Ark")
			assert.NotContains(t, r.Title, "Animated.Series")
			assert.NotContains(t, r.Title, "Norway")
			assert.NotContains(t, r.Title, "S01E02")
		}
	})

	t.Run("Identifier-matched releases trust the indexer mapping and bypass the title gate", func(t *testing.T) {
		results, err := coord.Search(ctx, SearchParams{
			Type:      "series",
			IMDBID:    "tt15367376",
			TVDBID:    "415089",
			Title:     "The Ark",
			Season:    1,
			Episode:   1,
			TimeoutMS: 2000,
		})
		require.NoError(t, err)

		// The indexer matched by tvdbid/imdbid, so every release it returned
		// is trusted (Prowlarr/Sonarr/Radarr semantics) even when release
		// names do not clean-match the requested title.
		require.Len(t, results, 6)
		for _, r := range results {
			assert.True(t, r.ByIDSearch)
		}
	})

	t.Run("SearchInspect returns both active and discarded releases with evaluation reasons", func(t *testing.T) {
		coordWithExcludes := NewSearchCoordinator(CoordinatorConfig{
			Provider: "newsnab",
			NewsnabIndexers: []newsnab.IndexerConfig{
				{
					Name:    "mock-indexer",
					URL:     server.URL,
					APIKey:  "dummy-key",
					Enabled: true,
				},
			},
			Scoring: StreamScoringConfig{
				ExcludeKeywords: []string{"H.264"},
				CustomFormats: []TrashCustomFormat{
					{
						ID:       "webdl_2160p",
						Name:     "4K WEB-DL",
						Pattern:  `\b2160p\b`,
						Score:    300,
						Enabled:  true,
					},
				},
			},
		}, server.Client())

		inspect, err := coordWithExcludes.SearchInspect(ctx, SearchParams{
			Type:      "series",
			Title:     "The Ark",
			Season:    1,
			Episode:   1,
			TimeoutMS: 2000,
		})
		require.NoError(t, err)
		assert.Equal(t, 6, inspect.TotalResults)
		// Active should only be The Ark 2160p (since 1080p has H.264 keyword excluded, and others are wrong series/episodes)
		assert.Equal(t, 1, inspect.ActiveResults)
		assert.Equal(t, 5, inspect.DiscardedResults)

		// Verify active release
		assert.False(t, inspect.Releases[0].Excluded)
		assert.Contains(t, inspect.Releases[0].Title, "The.Ark.2023.S01E01.2160p")
		assert.Equal(t, 300, inspect.Releases[0].Score)

		// Verify excluded release has reason
		foundH264Exclude := false
		foundMismatch := false
		for _, rel := range inspect.Releases[1:] {
			assert.True(t, rel.Excluded)
			if strings.Contains(rel.Title, "H.264") {
				foundH264Exclude = true
				assert.Contains(t, rel.ExcludeReason, "H.264")
			}
			if strings.Contains(rel.Title, "Animated.Series") {
				foundMismatch = true
				assert.Contains(t, rel.ExcludeReason, "Does not match")
			}
		}
		assert.True(t, foundH264Exclude, "Should have marked H.264 release as excluded with reason")
		assert.True(t, foundMismatch, "Should have marked mismatch series as excluded with reason")
	})
}

// newsnabItemXML builds a minimal Newznab RSS item so a stubbed indexer can
// return a hit for the ID-based query form.
func newsnabItemXML(title string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>t</title>` +
		`<item><title>` + title + `</title><link>http://example.invalid/nzb/1</link>` +
		`<enclosure url="http://example.invalid/nzb/1" length="1000" type="application/x-nzb"/>` +
		`</item></channel></rss>`
}

// TestSearchInspect_TitleQueryOnlyWhenIDSearchIsEmpty pins that the broader
// free-text title query is a fallback, not an unconditional second round. Firing
// both forms on every request doubles each indexer's request volume, and
// indexers rate-limit aggressively.
func TestSearchInspect_TitleQueryOnlyWhenIDSearchIsEmpty(t *testing.T) {
	t.Run("ID hit suppresses the title query", func(t *testing.T) {
		var queries []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("t") == "caps" {
				w.Header().Set("Content-Type", "application/xml")
				_, _ = w.Write([]byte(testCapsXML))
				return
			}
			queries = append(queries, r.URL.RawQuery)
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(newsnabItemXML("The.Ark.S01E01.1080p.WEB.H264")))
		}))
		defer server.Close()

		sc := NewSearchCoordinator(CoordinatorConfig{
			Provider: "newsnab",
			NewsnabIndexers: []newsnab.IndexerConfig{
				{Name: "idx", URL: server.URL, APIKey: "k", Enabled: true},
			},
		}, server.Client())

		res, err := sc.SearchInspect(context.Background(), SearchParams{
			Type: "series", IMDBID: "tt15367376", TVDBID: "415089",
			Title: "The Ark", Season: 1, Episode: 1, TimeoutMS: 2000,
		})
		require.NoError(t, err)
		require.NotNil(t, res)

		assert.Len(t, queries, 1, "expected only the ID query, got %v", queries)
		for _, q := range queries {
			assert.NotContains(t, q, "q=The", "title query must not run when the ID search found results")
		}
	})

	t.Run("empty ID result falls back to the title query", func(t *testing.T) {
		var queries []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("t") == "caps" {
				w.Header().Set("Content-Type", "application/xml")
				_, _ = w.Write([]byte(testCapsXML))
				return
			}
			queries = append(queries, r.URL.RawQuery)
			w.Header().Set("Content-Type", "application/xml")
			if strings.Contains(r.URL.RawQuery, "q=") {
				_, _ = w.Write([]byte(newsnabItemXML("The.Ark.S01E01.1080p.WEB.H264")))
				return
			}
			_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>t</title></channel></rss>`))
		}))
		defer server.Close()

		sc := NewSearchCoordinator(CoordinatorConfig{
			Provider: "newsnab",
			NewsnabIndexers: []newsnab.IndexerConfig{
				{Name: "idx", URL: server.URL, APIKey: "k", Enabled: true},
			},
		}, server.Client())

		res, err := sc.SearchInspect(context.Background(), SearchParams{
			Type: "series", IMDBID: "tt15367376", TVDBID: "415089",
			Title: "The Ark", Season: 1, Episode: 1, TimeoutMS: 2000,
		})
		require.NoError(t, err)
		require.NotNil(t, res)

		// The identifier search degrades through its ladder (full params,
		// minus ep, minus season, minus categories). A still-empty bare
		// identifier search is learned as unsupported and answered inside
		// the same client call by a keyword retry on the raw identifier —
		// so the ID pass costs five queries and satisfies the coordinator,
		// which then never needs the title fallback.
		require.Len(t, queries, 5, "expected the degrading ID queries then the learned keyword retry, got %v", queries)
		assert.Contains(t, queries[0], "tvdbid=415089", "first query should be the identifier query")
		assert.Contains(t, queries[3], "tvdbid=415089", "the bare identifier query precedes the fallback")
		assert.Contains(t, queries[4], "q=tt15367376", "last query should be the client keyword retry")
		assert.Equal(t, 1, res.TotalResults, "the fallback hit should be evaluated")
	})
}

// counterattackFeedXML models the tt23648788 report: the indexer matched
// releases via imdbid even though release names use a different title than
// the Cinemeta-resolved one ("Contraataque").
const counterattackFeedXML = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
  <channel>
    <title>Mock Indexer</title>
    <item>
      <title>Counterattack.2025.1080p.WEB-DL.DDP5.1.H.264-EVOLVE</title>
      <link>https://indexer.example/dl/1.nzb</link>
      <enclosure url="https://indexer.example/dl/1.nzb" length="2500000000" type="application/x-nzb" />
    </item>
    <item>
      <title>Counterattack.2025.2160p.UHD.BluRay.REMUX.HDR.HEVC-FLUX</title>
      <link>https://indexer.example/dl/2.nzb</link>
      <enclosure url="https://indexer.example/dl/2.nzb" length="55000000000" type="application/x-nzb" />
    </item>
  </channel>
</rss>`

func TestSearch_Movie_IDMatchedReleases_BypassTitleGate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("t") == "caps" {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(testCapsXML))
			return
		}

		w.Header().Set("Content-Type", "application/xml")
		if q.Get("imdbid") != "" || (q.Get("t") == "search" && q.Get("q") == "Contraataque") {
			_, _ = w.Write([]byte(counterattackFeedXML))
			return
		}
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>mock</title></channel></rss>`))
	}))
	defer server.Close()

	coord := NewSearchCoordinator(CoordinatorConfig{
		Provider: "newsnab",
		NewsnabIndexers: []newsnab.IndexerConfig{
			{Name: "mock-indexer", URL: server.URL, APIKey: "dummy-key", Enabled: true},
		},
	}, server.Client())

	ctx := context.Background()

	t.Run("ID-matched foreign-title releases stay active", func(t *testing.T) {
		inspect, err := coord.SearchInspect(ctx, SearchParams{
			Type:      "movie",
			IMDBID:    "tt23648788",
			Title:     "Contraataque",
			TimeoutMS: 4000,
		})
		require.NoError(t, err)
		require.Equal(t, 2, inspect.ActiveResults)
		for _, rel := range inspect.Releases {
			if !rel.Excluded {
				assert.Contains(t, rel.Title, "Counterattack")
				assert.True(t, rel.ByIDSearch)
			}
		}
	})

	t.Run("Keyword results with mismatched titles are still excluded", func(t *testing.T) {
		inspect, err := coord.SearchInspect(ctx, SearchParams{
			Type:      "movie",
			Title:     "Contraataque",
			TimeoutMS: 4000,
		})
		require.NoError(t, err)
		require.Equal(t, 2, inspect.TotalResults)
		assert.Equal(t, 0, inspect.ActiveResults)
		assert.Equal(t, 2, inspect.DiscardedResults)
		for _, rel := range inspect.Releases {
			assert.False(t, rel.ByIDSearch)
			assert.Contains(t, rel.ExcludeReason, "Does not match requested movie title")
		}
	})
}

func TestSearch_Movie_EmptyIDResult_FallsBackToKeywordQuery(t *testing.T) {
	var mu sync.Mutex
	var queries []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("t") == "caps" {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(testCapsXML))
			return
		}

		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/xml")
		if q.Get("t") == "movie" && q.Get("imdbid") != "" {
			// Identifier query supported but the indexer has no mapping.
			_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>mock</title></channel></rss>`))
			return
		}
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Mock Indexer</title>
    <item>
      <title>Contraataque.2025.2160p.WEB-DL.DDP5.1.H.265-FLUX</title>
      <link>https://indexer.example/dl/3.nzb</link>
      <enclosure url="https://indexer.example/dl/3.nzb" length="55000000000" type="application/x-nzb" />
    </item>
  </channel>
</rss>`))
	}))
	defer server.Close()

	coord := NewSearchCoordinator(CoordinatorConfig{
		Provider: "newsnab",
		NewsnabIndexers: []newsnab.IndexerConfig{
			{Name: "mock-indexer", URL: server.URL, APIKey: "dummy-key", Enabled: true},
		},
	}, server.Client())

	results, err := coord.Search(context.Background(), SearchParams{
		Type:      "movie",
		IMDBID:    "tt23648788",
		Title:     "Contraataque",
		TimeoutMS: 4000,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Title, "Contraataque.")
	assert.False(t, results[0].ByIDSearch)

	mu.Lock()
	defer mu.Unlock()
	imdbIdx, keywordIdx := -1, -1
	for i, query := range queries {
		if strings.Contains(query, "imdbid=") && imdbIdx == -1 {
			imdbIdx = i
		}
		if strings.Contains(query, "q=Contraataque") && !strings.Contains(query, "imdbid=") {
			keywordIdx = i
		}
	}
	require.NotEqual(t, -1, imdbIdx, "identifier query must have been issued first")
	require.NotEqual(t, -1, keywordIdx, "keyword fallback query must have been issued")
	assert.Greater(t, keywordIdx, imdbIdx, "keyword fallback must follow the empty identifier query")
}
