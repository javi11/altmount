package newsnab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
		<search available="yes"/>
		<movie-search available="yes"/>
		<tv-search available="yes"/>
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
	<supportedParams>q,imdbid,tvdbid,season,ep,extended,limit,cat</supportedParams>
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
		assert.True(t, caps.supportsParam("imdbid"))
		assert.False(t, caps.supportsParam("tmdbid"))
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
		<search available="yes"/>
		<tv-search available="yes"/>
	</searching>
	<supportedParams>q,imdbid,season,ep</supportedParams>
</caps>`))
			return
		}
		q := r.URL.Query()
		if _, ok := q["tvdbid"]; ok {
			tvdbRequested = true
		}
		assert.Equal(t, "15367376", q.Get("imdbid"))
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>x</title></channel></rss>`))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{Name: "tv-caps-idx", URL: ts.URL, APIKey: "k", Enabled: true}, ts.Client())
	_, err := client.SearchTV(context.Background(), "tt15367376", "415089", "The Ark", 1, 1, nil, "ua")
	require.NoError(t, err)
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
