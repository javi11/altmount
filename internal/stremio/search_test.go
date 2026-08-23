package stremio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/javi11/altmount/internal/newsnab"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchTV_NewznabQueryPriority_Prowlarr1to1(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>test</title></channel></rss>`))
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

	t.Run("Searching for The Ark (Syfy) strictly retains The Ark and rejects ARK The Animated Series", func(t *testing.T) {
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

		// Expect only the two genuine The Ark S01E01 releases
		require.Len(t, results, 2)
		for _, r := range results {
			assert.Contains(t, r.Title, "The.Ark")
			assert.NotContains(t, r.Title, "Animated.Series")
			assert.NotContains(t, r.Title, "Norway")
			assert.NotContains(t, r.Title, "S01E02")
		}
	})

	t.Run("Searching for ARK: The Animated Series strictly retains only Animated Series releases", func(t *testing.T) {
		results, err := coord.Search(ctx, SearchParams{
			Type:      "series",
			IMDBID:    "tt17371078",
			TVDBID:    "393699",
			Title:     "ARK: The Animated Series",
			Season:    1,
			Episode:   1,
			TimeoutMS: 2000,
		})
		require.NoError(t, err)

		// Expect only the two ARK The Animated Series releases
		require.Len(t, results, 2)
		for _, r := range results {
			assert.True(t, strings.Contains(r.Title, "ARK.The.Animated.Series") || strings.Contains(r.Title, "ARK - The Animated Series"))
			assert.NotContains(t, r.Title, "The.Ark.S01E01")
			assert.NotContains(t, r.Title, "The.Ark.2023")
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

		require.Len(t, queries, 2, "expected the ID query then the title fallback, got %v", queries)
		assert.Contains(t, queries[1], "q=The", "second query should be the title fallback")
		assert.Equal(t, 1, res.TotalResults, "the fallback hit should be evaluated")
	})
}
