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
}
