package newsnab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSlowCapsServer serves a caps document only after capsDelay, while search
// queries answer immediately. It models an indexer whose caps endpoint is
// degraded but whose search endpoint is healthy.
func newSlowCapsServer(t *testing.T, capsDelay time.Duration) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var searches atomic.Int32
	// stop releases any handler still parked on capsDelay so teardown does not
	// block on the detached background fetch.
	stop := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			select {
			case <-time.After(capsDelay):
			case <-r.Context().Done():
				return
			case <-stop:
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><server title="slow"/><searching><search available="yes"/><movie-search available="yes"/></searching><supportedParams>q,imdbid</supportedParams></caps>`))
			return
		}
		searches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"channel":{"title":"idx","item":[{"title":"Some.Movie.2025.1080p.WEB-DL","link":"https://i.test/a.nzb","enclosure":{"_url":"https://i.test/a.nzb","_length":"100"}}]}}`))
	}))
	t.Cleanup(func() {
		close(stop)
		ts.Close()
	})
	return ts, &searches
}

// A caps endpoint far slower than the search itself must not consume the
// caller's search budget: the query degrades to unknown caps and still runs.
func TestNewsnabClient_SlowCaps_DoesNotStarveSearch(t *testing.T) {
	ts, searches := newSlowCapsServer(t, 30*time.Second)

	client := NewClient(IndexerConfig{
		Name: "slow-caps", URL: ts.URL, APIKey: "k", Enabled: true, TimeoutSeconds: 30,
	}, ts.Client())

	// A search budget comfortably larger than capsWaitBudget but far smaller
	// than the caps delay: only a shared deadline could exhaust it.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	results, err := client.SearchMovie(ctx, "tt1234567", "Some Movie", nil, "ua")
	elapsed := time.Since(start)

	require.NoError(t, err, "search must degrade to unknown caps, not fail with the caps endpoint")
	require.Len(t, results, 1)
	assert.Equal(t, int32(1), searches.Load(), "the search query itself must still be issued")
	assert.Less(t, elapsed, capsWaitBudget+time.Second,
		"search waited on the caps endpoint instead of degrading (took %s)", elapsed)
}

// The caps lock must not be held across the caps HTTP round trip: concurrent
// searches against one indexer must not serialize behind a single slow fetch.
func TestNewsnabClient_SlowCaps_ConcurrentSearchesNotSerialized(t *testing.T) {
	ts, _ := newSlowCapsServer(t, 30*time.Second)

	client := NewClient(IndexerConfig{
		Name: "slow-caps", URL: ts.URL, APIKey: "k", Enabled: true, TimeoutSeconds: 30,
	}, ts.Client())

	const concurrency = 5
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errs := make([]error, concurrency)
	start := time.Now()
	for i := range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = client.SearchMovie(ctx, "tt1234567", "Some Movie", nil, "ua")
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	for i, err := range errs {
		require.NoErrorf(t, err, "concurrent search %d failed", i)
	}
	// Serialized behaviour would be concurrency * capsWaitBudget or worse;
	// single-flighted waiting is one budget for all of them.
	assert.Less(t, elapsed, capsWaitBudget+time.Second,
		"concurrent searches serialized behind the caps fetch (took %s)", elapsed)
}

// A caps failure is negatively cached and must not be refetched on every
// search, and the background refresh must still populate the cache after the
// caller that triggered it has already given up.
func TestNewsnabClient_SlowCaps_BackgroundFetchWarmsCache(t *testing.T) {
	var capsHits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			capsHits.Add(1)
			time.Sleep(capsWaitBudget + 300*time.Millisecond)
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><server title="slow"/><searching><search available="yes"/><movie-search available="yes"/></searching><supportedParams>q</supportedParams></caps>`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"channel":{"title":"idx","item":[]}}`))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{
		Name: "warm", URL: ts.URL, APIKey: "k", Enabled: true, TimeoutSeconds: 30,
	}, ts.Client())

	// First search gives up on caps and degrades.
	_, err := client.SearchMovie(context.Background(), "tt1234567", "Some Movie", nil, "ua")
	require.NoError(t, err)
	assert.Nil(t, client.cachedCaps(), "caps should still be unknown while the fetch is in flight")

	// The detached fetch completes even though its originating caller moved on.
	require.Eventually(t, func() bool { return client.cachedCaps() != nil },
		5*time.Second, 25*time.Millisecond, "background caps fetch never populated the cache")

	// A subsequent search reuses the warmed cache rather than refetching.
	_, err = client.SearchMovie(context.Background(), "tt1234567", "Some Movie", nil, "ua")
	require.NoError(t, err)
	assert.Equal(t, int32(1), capsHits.Load(), "caps must be fetched once, then served from cache")
}

func TestParseCaps_PerSearchTypeSupportedParams(t *testing.T) {
	t.Run("Prowlarr parity: available without supportedParams means q only", func(t *testing.T) {
		doc := `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<server appversion="" version="0.1"/>
	<limits max="60" default="25"/>
	<searching>
		<search available="yes"/>
		<tv-search available="yes"/>
		<movie-search available="yes"/>
	</searching>
	<categories>
		<category id="5000" name="TV">
			<subcat id="5070" name="Anime"/>
		</category>
	</categories>
</caps>`
		caps := parseCaps([]byte(doc))
		require.NotNil(t, caps)
		assert.True(t, caps.TVSearch)
		assert.True(t, caps.supportsParam(SearchTypeTV, "q"))
		assert.False(t, caps.supportsParam(SearchTypeTV, "ep"), "undeclared params must not be assumed")
		assert.False(t, caps.supportsParam(SearchTypeTV, "imdbid"))
		assert.False(t, caps.supportsParam(SearchTypeMovie, "imdbid"))
		assert.Equal(t, []string{"q"}, caps.TVParams)
	})

	t.Run("althub style: supportedParams attribute is parsed per search type", func(t *testing.T) {
		doc := `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<server title="altHUB"/>
	<searching>
		<search available="yes"/>
		<tv-search available="yes" supportedParams="q,rid,tvdbid,imdbid,tvmazeid,season,ep"/>
		<movie-search available="yes" supportedParams="q,imdbid"/>
	</searching>
</caps>`
		caps := parseCaps([]byte(doc))
		require.NotNil(t, caps)
		assert.True(t, caps.TVSearch)
		for _, p := range []string{"q", "tvdbid", "imdbid", "season", "ep"} {
			assert.True(t, caps.supportsParam(SearchTypeTV, p), "tv param %s", p)
		}
		assert.True(t, caps.supportsParam(SearchTypeMovie, "imdbid"))
		assert.False(t, caps.supportsParam(SearchTypeMovie, "season"))
	})

	t.Run("unavailable search function supports nothing", func(t *testing.T) {
		doc := `<?xml version="1.0"?><caps><searching><search available="yes"/><tv-search available="no" supportedParams="q,ep"/><movie-search available="no"/></searching></caps>`
		caps := parseCaps([]byte(doc))
		require.NotNil(t, caps)
		assert.False(t, caps.TVSearch)
		assert.False(t, caps.MovieSearch)
		assert.Nil(t, caps.TVParams)
	})

	t.Run("missing searching section assumes standard parameter sets", func(t *testing.T) {
		doc := `<?xml version="1.0"?><caps><server title="Minimal"/></caps>`
		caps := parseCaps([]byte(doc))
		require.NotNil(t, caps)
		assert.True(t, caps.Search)
		assert.True(t, caps.MovieSearch)
		assert.True(t, caps.TVSearch)
		assert.True(t, caps.supportsParam(SearchTypeTV, "season"))
		assert.True(t, caps.supportsParam(SearchTypeTV, "ep"))
		assert.True(t, caps.supportsParam(SearchTypeMovie, "imdbid"))
		assert.False(t, caps.supportsParam(SearchTypeMovie, "season"))
	})
}
