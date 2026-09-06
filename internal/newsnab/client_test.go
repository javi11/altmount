package newsnab

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCapsXML advertises every search type and parameter so existing tests
// exercise the legacy query shapes unchanged.
const testCapsXML = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<server title="Test Indexer"/>
	<searching>
		<search available="yes" supportedParams="q,cat,limit,extended"/>
		<movie-search available="yes" supportedParams="q,cat,imdbid,limit,extended"/>
		<tv-search available="yes" supportedParams="q,cat,imdbid,tvdbid,season,ep,limit,extended"/>
	</searching>
	<categories>
		<category id="2000" name="Movies">
			<subcat id="2010"/>
			<subcat id="2030"/>
			<subcat id="2040"/>
			<subcat id="2045"/>
			<subcat id="2060"/>
		</category>
		<category id="5000" name="TV">
			<subcat id="5010"/>
			<subcat id="5030"/>
			<subcat id="5040"/>
		</category>
	</categories>
</caps>`

// serveCaps routes ?t=caps requests to the given fixture inside test handlers.
func serveCaps(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Query().Get("t") != "caps" {
		return false
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write([]byte(testCapsXML))
	return true
}

func TestNewsnabClient_SearchMovie_JSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveCaps(w, r) {
			return
		}
		assert.Equal(t, "movie", r.URL.Query().Get("t"))
		assert.Equal(t, "1234567", r.URL.Query().Get("imdbid"))
		assert.Equal(t, "testkey", r.URL.Query().Get("apikey"))
		assert.Equal(t, "Radarr/6.5.1.2032 (alpine 3.23.3)", r.Header.Get("User-Agent"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"channel": {
				"title": "NZB Indexer",
				"item": [
					{
						"title": "Dune.Part.Two.2024.2160p.UHD.Remux.DV.HDR10+.TrueHD.Atmos.7.1-FraMeSToR",
						"link": "https://indexer.test/getnzb/1.nzb",
						"pubDate": "Mon, 10 Jun 2024 12:00:00 +0000",
						"enclosure": {
							"_url": "https://indexer.test/getnzb/1.nzb",
							"_length": "58720257000",
							"_type": "application/x-nzb"
						},
						"attr": [
							{"_name": "category", "_value": "2045"}
						]
					}
				]
			}
		}`))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{
		ID:             "test_idx",
		Name:           "Test Indexer",
		URL:            ts.URL,
		APIKey:         "testkey",
		TimeoutSeconds: 2,
		Enabled:        true,
	}, ts.Client())

	results, err := client.SearchMovie(context.Background(), "tt1234567", "", nil, "Radarr/6.5.1.2032 (alpine 3.23.3)")
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Dune.Part.Two.2024.2160p.UHD.Remux.DV.HDR10+.TrueHD.Atmos.7.1-FraMeSToR", results[0].Title)
	assert.Equal(t, "https://indexer.test/getnzb/1.nzb", results[0].DownloadURL)
	assert.Equal(t, int64(58720257000), results[0].Size)
	assert.Equal(t, "Test Indexer", results[0].Indexer)
	assert.True(t, results[0].ByIDSearch)
}

func TestNewsnabClient_SearchTV_XML(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveCaps(w, r) {
			return
		}
		assert.Equal(t, "tvsearch", r.URL.Query().Get("t"))
		assert.Equal(t, "Arcane", r.URL.Query().Get("q"))
		assert.Equal(t, "2", r.URL.Query().Get("season"))
		assert.Equal(t, "1", r.URL.Query().Get("ep"))

		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
		<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
			<channel>
				<title>Test TV Indexer</title>
				<item>
					<title>Arcane.S02E01.2160p.HDR.DDP5.1.Atmos.H.265-FLUX</title>
					<link>https://indexer.test/getnzb/2.nzb</link>
					<pubDate>Mon, 10 Jun 2024 14:00:00 +0000</pubDate>
					<enclosure url="https://indexer.test/getnzb/2.nzb" length="2500000000" type="application/x-nzb" />
					<newznab:attr name="category" value="5045" />
					<newznab:attr name="size" value="2500000000" />
				</item>
			</channel>
		</rss>`))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{
		ID:             "test_idx",
		Name:           "Test TV Indexer",
		URL:            ts.URL,
		APIKey:         "testkey",
		TimeoutSeconds: 2,
		Enabled:        true,
	}, ts.Client())

	results, err := client.SearchTV(context.Background(), "", "", "Arcane", 2, 1, nil, "Sonarr/4.1.1.824 (alpine 3.23.3)")
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Arcane.S02E01.2160p.HDR.DDP5.1.Atmos.H.265-FLUX", results[0].Title)
	assert.Equal(t, int64(2500000000), results[0].Size)
}

func TestNewsnabClient_SearchGeneral_NZBGeek_JSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveCaps(w, r) {
			return
		}
		assert.Equal(t, "search", r.URL.Query().Get("t"))
		assert.Equal(t, "Gladiator II", r.URL.Query().Get("q"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"@attributes": {"version": "1.0"},
			"channel": {
				"title": "api.nzbgeek.info",
				"item": [
					{
						"title": "Gladiator.II.2024.720p.AMZN.WEB-DL.DDP5.1.H.264-ViSTA",
						"guid": "https://nzbgeek.info/geekseek.php?guid=1fc90df05debd7d37615bc1638aa3389",
						"link": "https://api.nzbgeek.info/api?t=get&id=1fc90df05debd7d37615bc1638aa3389&apikey=testkey",
						"pubDate": "Thu, 13 Aug 2026 01:18:34 +0000",
						"enclosure": {
							"@attributes": {
								"url": "http://api.nzbgeek.info/api?t=get&id=1fc90df05debd7d37615bc1638aa3389&apikey=testkey",
								"length": "5471988000",
								"type": "application/x-nzb"
							}
						},
						"attr": [
							{"@attributes": {"name": "category", "value": "2000"}},
							{"@attributes": {"name": "size", "value": "5471988000"}}
						]
					}
				]
			}
		}`))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{
		ID:             "nzbgeek",
		Name:           "nzbgeek",
		URL:            ts.URL,
		APIKey:         "testkey",
		TimeoutSeconds: 2,
		Enabled:        true,
	}, ts.Client())

	results, err := client.SearchGeneral(context.Background(), "Gladiator II", []int{2000, 2010, 2030}, "Altmount/1.0")
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Gladiator.II.2024.720p.AMZN.WEB-DL.DDP5.1.H.264-ViSTA", results[0].Title)
	assert.Equal(t, int64(5471988000), results[0].Size)
	assert.Equal(t, "http://api.nzbgeek.info/api?t=get&id=1fc90df05debd7d37615bc1638aa3389&apikey=testkey", results[0].DownloadURL)
	assert.False(t, results[0].ByIDSearch)
}

func TestParseNewsnabTime(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantOK  bool
		wantUTC string
	}{
		{"RFC1123Z", "Thu, 13 Aug 2026 01:18:34 +0000", true, "2026-08-13T01:18:34Z"},
		{"RFC1123 GMT", "Thu, 13 Aug 2026 01:18:34 GMT", true, "2026-08-13T01:18:34Z"},
		{"RFC3339", "2026-08-13T01:18:34Z", true, "2026-08-13T01:18:34Z"},
		{"RFC3339 with offset", "2026-08-13T03:18:34+02:00", true, "2026-08-13T01:18:34Z"},
		{"ISO without zone", "2026-08-13T01:18:34", true, "2026-08-13T01:18:34Z"},
		{"SQL style", "2026-08-13 01:18:34", true, "2026-08-13T01:18:34Z"},
		{"empty", "", false, ""},
		{"garbage", "not-a-date", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNewsnabTime(tt.value)
			if !tt.wantOK {
				assert.True(t, got.IsZero(), "expected zero time for %q", tt.value)
				return
			}
			assert.False(t, got.IsZero())
			assert.Equal(t, tt.wantUTC, got.UTC().Format(time.RFC3339))
		})
	}
}

func TestParseCaps(t *testing.T) {
	t.Run("Full caps document", func(t *testing.T) {
		caps := parseCaps([]byte(testCapsXML))
		require.NotNil(t, caps)
		assert.Equal(t, "Test Indexer", caps.ServerName)
		assert.True(t, caps.Search)
		assert.True(t, caps.MovieSearch)
		assert.True(t, caps.TVSearch)
		assert.Contains(t, caps.Categories, 2000)
		assert.Contains(t, caps.Categories, 2045)
		assert.Contains(t, caps.Categories, 5000)
		assert.Contains(t, caps.Categories, 5040)
		assert.True(t, caps.supportsParam(SearchTypeTV, "imdbid"))
		assert.True(t, caps.supportsParam(SearchTypeTV, "season"))
		assert.True(t, caps.supportsParam(SearchTypeMovie, "imdbid"))
		assert.False(t, caps.supportsParam(SearchTypeTV, "tmdbid"))
		assert.False(t, caps.supportsParam(SearchTypeMovie, "season"))
	})

	t.Run("Missing searching section means unknown not unsupported", func(t *testing.T) {
		doc := `<?xml version="1.0"?><caps><server title="Minimal"/></caps>`
		caps := parseCaps([]byte(doc))
		require.NotNil(t, caps)
		assert.True(t, caps.Search)
		assert.True(t, caps.MovieSearch)
		assert.True(t, caps.TVSearch)
	})

	t.Run("RSS body is rejected", func(t *testing.T) {
		rss := `<?xml version="1.0"?><rss version="2.0"><channel><title>x</title></channel></rss>`
		assert.Nil(t, parseCaps([]byte(rss)))
	})
}

func TestFilterCategories(t *testing.T) {
	caps := parseCaps([]byte(testCapsXML))
	require.NotNil(t, caps)

	t.Run("Declared subcategories are kept", func(t *testing.T) {
		assert.Equal(t, []int{2045}, filterCategories(caps, []int{2045}))
	})

	t.Run("Undeclared subcategory kept when parent category is declared", func(t *testing.T) {
		assert.Equal(t, []int{2090}, filterCategories(caps, []int{2090}))
	})

	t.Run("Disjoint tree drops the constraint entirely", func(t *testing.T) {
		assert.Nil(t, filterCategories(caps, []int{7000, 7010}))
	})

	t.Run("Unknown caps pass through unchanged", func(t *testing.T) {
		requested := []int{2000, 7000}
		assert.Equal(t, requested, filterCategories(nil, requested))
	})
}

func TestDetectAPIError(t *testing.T) {
	t.Run("XML error payload", func(t *testing.T) {
		body := []byte(`<?xml version="1.0"?><error code="203" description="Function not available"/>`)
		err := detectAPIError(body)
		require.Error(t, err)
		var apiErr *Error
		assert.ErrorAs(t, err, &apiErr)
		assert.Equal(t, 203, apiErr.Code)
		assert.Equal(t, "Function not available", apiErr.Description)
		assert.Contains(t, err.Error(), "203")
	})

	t.Run("JSON error payload", func(t *testing.T) {
		body := []byte(`{"error":{"@attributes":{"code":"101","description":"Incorrect user credentials"}}}`)
		err := detectAPIError(body)
		require.Error(t, err)
		var apiErr *Error
		assert.ErrorAs(t, err, &apiErr)
		assert.Equal(t, 101, apiErr.Code)
	})

	t.Run("Successful payloads yield no error", func(t *testing.T) {
		rss := []byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>x</title></channel></rss>`)
		assert.NoError(t, detectAPIError(rss))
		jsonBody := []byte(`{"channel":{"title":"x","item":[]}}`)
		assert.NoError(t, detectAPIError(jsonBody))
	})
}

func TestNewsnabClient_SearchMovie_CapsWithoutImdbID_FallsBackToKeyword(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<server title="No IMDB Indexer"/>
	<searching>
		<search available="yes"/>
		<movie-search available="yes"/>
	</searching>
	<supportedParams>q,season,ep</supportedParams>
</caps>`))
			return
		}
		q := r.URL.Query()
		assert.Equal(t, "movie", q.Get("t"))
		assert.Empty(t, q.Get("imdbid"))
		assert.Equal(t, "Contraataque", q.Get("q"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"channel":{"title":"idx","item":[{"title":"Counterattack.2025.1080p.WEB-DL-EVOLVE","link":"https://i.test/a.nzb","enclosure":{"_url":"https://i.test/a.nzb","_length":"100"}}]}}`))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{Name: "caps-idx", URL: ts.URL, APIKey: "k", Enabled: true}, ts.Client())
	results, err := client.SearchMovie(context.Background(), "tt23648788", "Contraataque", nil, "ua")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].ByIDSearch)
}

func TestNewsnabClient_SearchTV_TVDBIDUnsupported_PrefersImdbID(t *testing.T) {
	tvdbRequested := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<server title="No TVDB Indexer"/>
	<searching>
		<search available="yes" supportedParams="q"/>
		<tv-search available="yes" supportedParams="q,imdbid,season,ep"/>
	</searching>
</caps>`))
			return
		}
		q := r.URL.Query()
		if _, ok := q["tvdbid"]; ok {
			tvdbRequested = true
		}
		if q.Get("imdbid") != "" {
			assert.Equal(t, "15367376", q.Get("imdbid"))
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>x</title><item><title>The Ark S01E01</title><enclosure url="http://idx/nzb.nzb" type="application/x-nzb"/></item></channel></rss>`))
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>x</title></channel></rss>`))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{Name: "tv-caps-idx", URL: ts.URL, APIKey: "k", Enabled: true}, ts.Client())
	results, err := client.SearchTV(context.Background(), "tt15367376", "415089", "The Ark", 1, 1, nil, "ua")
	require.NoError(t, err)
	require.NotEmpty(t, results, "identifier search must succeed without triggering degradation")
	assert.False(t, tvdbRequested, "tvdbid must be omitted when caps do not advertise it")
}

func TestNewsnabClient_SearchSurfacesNewznabAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveCaps(w, r) {
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><error code="201" description="Missing parameter"/>`))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{Name: "err-idx", URL: ts.URL, APIKey: "k", Enabled: true}, ts.Client())
	_, err := client.SearchMovie(context.Background(), "tt1234567", "", nil, "ua")
	require.Error(t, err)

	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 201, apiErr.Code)
	assert.Equal(t, "Missing parameter", apiErr.Description)
	assert.True(t, strings.Contains(err.Error(), "201"))
}

func TestSanitizeSearchQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "colon title", input: "Re:ZERO -Starting Life in Another World-", want: "Re ZERO -Starting Life in Another World-"},
		{name: "multiple colons collapse spaces", input: "Re:Zero::Part:2", want: "Re Zero Part 2"},
		{name: "comma and semicolon", input: "Show, Part; One", want: "Show Part One"},
		{name: "keeps scene dots and dashes", input: "One.Piece.S004E111-FLUX", want: "One.Piece.S004E111-FLUX"},
		{name: "trims and collapses whitespace", input: "  A   Will   Eternal  ", want: "A Will Eternal"},
		{name: "only punctuation", input: "::", want: ""},
		{name: "imdb id untouched", input: "tt5566766", want: "tt5566766"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeSearchQuery(tt.input))
		})
	}
}

func TestQueryFallbackSanitizesTitle(t *testing.T) {
	assert.Equal(t, "Re ZERO kara Hajimeru", queryFallback("", "Re:ZERO kara Hajimeru"))
	// Identifier passthrough is never rewritten.
	assert.Equal(t, "tt5566766", queryFallback("tt5566766", ""))
}

// rssWithItems builds a minimal RSS response containing the given titles.
func rssWithItems(titles ...string) string {
	items := ""
	for _, title := range titles {
		items += `<item><title>` + title + `</title><enclosure url="http://idx/` + title + `.nzb" type="application/x-nzb"/></item>`
	}
	return `<?xml version="1.0"?><rss version="2.0"><channel><title>x</title>` + items + `</channel></rss>`
}

func TestNewsnabClient_SearchTV_DegradesWhenEpisodeFilterZeroes(t *testing.T) {
	var mu sync.Mutex
	searchQueries := []url.Values{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveCaps(w, r) {
			return
		}
		q := r.URL.Query()
		mu.Lock()
		searchQueries = append(searchQueries, q)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		// Model altHUB: an advertised ep filter that zeroes out for content
		// it cannot parse. Any query carrying ep returns nothing; the same
		// query without ep returns releases.
		if q.Get("ep") != "" {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>x</title></channel></rss>`))
			return
		}
		_, _ = w.Write([]byte(rssWithItems("Re Zero kara Hajimeru Isekai Seikatsu - 72")))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{Name: "ep-zero-idx", URL: ts.URL, APIKey: "k", Enabled: true}, ts.Client())
	results, err := client.SearchTV(context.Background(), "tt5566766", "", "Re Zero", 2, 11, nil, "ua")
	require.NoError(t, err)
	require.Len(t, results, 1)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, searchQueries, 2, "exactly one degradation retry expected")
	assert.Equal(t, "11", searchQueries[0].Get("ep"), "first attempt keeps the advertised ep filter")
	assert.Empty(t, searchQueries[1].Get("ep"), "retry must drop the zeroing ep filter")
	assert.Equal(t, "2", searchQueries[1].Get("season"), "season narrowing survives the first rung")
	assert.Equal(t, "5566766", searchQueries[1].Get("imdbid"))
	// Episode precision was dropped, so identifier trust must not apply.
	assert.False(t, results[0].ByIDSearch, "degraded identifier matches must be re-validated by media matching")
}

func TestNewsnabClient_SearchTV_NoRetryWhenFirstAttemptHasResults(t *testing.T) {
	var requests atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveCaps(w, r) {
			return
		}
		requests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(rssWithItems("Release One")))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{Name: "healthy-idx", URL: ts.URL, APIKey: "k", Enabled: true}, ts.Client())
	results, err := client.SearchTV(context.Background(), "tt1234567", "", "Show", 1, 1, nil, "ua")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, int32(1), requests.Load(), "non-empty first attempt must not retry")
}

func TestNewsnabClient_SearchMovie_DropsCategoriesWhenCombinedQueryZeroes(t *testing.T) {
	var mu sync.Mutex
	searchQueries := []url.Values{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveCaps(w, r) {
			return
		}
		q := r.URL.Query()
		mu.Lock()
		searchQueries = append(searchQueries, q)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		// Model NZBgeek per-type strictness: a movie identifier combined
		// with TV categories returns nothing; the identifier alone works.
		if q.Get("imdbid") != "" && q.Get("cat") != "" {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>x</title></channel></rss>`))
			return
		}
		_, _ = w.Write([]byte(rssWithItems("Some Movie 1080p")))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{Name: "strict-cats-idx", URL: ts.URL, APIKey: "k", Enabled: true}, ts.Client())
	// TV categories passed to a movie search.
	results, err := client.SearchMovie(context.Background(), "tt0111161", "Some Movie", []int{5000, 5030, 5040}, "ua")
	require.NoError(t, err)
	require.Len(t, results, 1)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, searchQueries, 2)
	assert.NotEmpty(t, searchQueries[0].Get("cat"))
	assert.Empty(t, searchQueries[1].Get("cat"), "retry must drop the zeroing category filter")
	assert.Equal(t, "0111161", searchQueries[1].Get("imdbid"))
	// The identifier still pins the movie exactly, so trust is preserved.
	assert.True(t, results[0].ByIDSearch)
}

func TestNewsnabClient_LearnsUnsupportedIdentifierSearch(t *testing.T) {
	var mu sync.Mutex
	searchQueries := []url.Values{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveCaps(w, r) {
			return
		}
		q := r.URL.Query()
		mu.Lock()
		searchQueries = append(searchQueries, q)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		// Model altHUB identifier behavior: imdbid queries always return
		// zero rows; keyword queries return releases.
		if q.Get("imdbid") != "" {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>x</title></channel></rss>`))
			return
		}
		_, _ = w.Write([]byte(rssWithItems("A Will Eternal - Season 4 Episode 7 [172] (1080p)")))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{Name: "no-mappings-idx", URL: ts.URL, APIKey: "k", Enabled: true}, ts.Client())

	// First request: identifier attempt, full degradation ladder, then the
	// keyword fallback answers.
	results, err := client.SearchTV(context.Background(), "tt11143630", "", "A Will Eternal", 4, 7, nil, "ua")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, client.idSearchBroken(idParamImdb) == false, "negative cache entry must be set")

	mu.Lock()
	firstCallRequests := len(searchQueries)
	mu.Unlock()

	// Second request: the learned entry skips identifier queries entirely.
	results, err = client.SearchTV(context.Background(), "tt11143630", "", "A Will Eternal", 4, 7, nil, "ua")
	require.NoError(t, err)
	require.Len(t, results, 1)

	mu.Lock()
	defer mu.Unlock()
	secondCallRequests := len(searchQueries) - firstCallRequests
	assert.Equal(t, 1, secondCallRequests, "learned indexer must answer with a single keyword query")
	assert.NotEmpty(t, searchQueries[firstCallRequests].Get("q"))
	assert.Empty(t, searchQueries[firstCallRequests].Get("imdbid"))
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewsnabClient_DownloadNZB_Redirect(t *testing.T) {
	const indexerURL = "https://indexer.example.com/api?t=get&id=123"
	const cdnURL = "https://cdn.example.com/nzbs/123.nzb"
	const nzbData = "<nzb>newsnab content</nzb>"

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case indexerURL:
			header := make(http.Header)
			header.Set("Location", cdnURL)
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		case cdnURL:
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(nzbData)),
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("not found")),
			}, nil
		}
	})

	httpClient := &http.Client{Transport: transport}
	client := NewClient(IndexerConfig{Name: "redirect-idx", URL: "https://indexer.example.com", APIKey: "k", Enabled: true}, httpClient)

	t.Run("redirect to cdn succeeds", func(t *testing.T) {
		data, err := client.DownloadNZB(context.Background(), indexerURL, "test-ua")
		require.NoError(t, err)
		assert.Equal(t, nzbData, string(data))
	})

	t.Run("private address is rejected", func(t *testing.T) {
		_, err := client.DownloadNZB(context.Background(), "http://10.0.0.1/nzb.xml", "test-ua")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "private address")
	})
}
