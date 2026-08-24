package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

// TestSeriesMetadataNegativeCaching pins that a lookup which finds nothing is
// remembered, so a series with no TVmaze entry does not re-issue outbound
// requests on every stream request. These resolvers sit on the Stremio hot
// path, called once per series request.
func TestSeriesMetadataNegativeCaching(t *testing.T) {
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	restore := overrideTVmazeBaseURL(srv.URL)
	defer restore()
	resetSeriesMetadataCaches()

	const imdb = "tt0000001"
	for i := 0; i < 5; i++ {
		if aliases := resolveSeriesTitleAliases(context.Background(), imdb); aliases != nil {
			t.Fatalf("expected nil aliases for an unknown series, got %v", aliases)
		}
	}

	if got := atomic.LoadInt64(&calls); got > 1 {
		t.Errorf("made %d outbound requests for a known-missing series, want at most 1 (negative result not cached)", got)
	}
}

// TestSeriesMetadataCacheIsBounded pins that the caches cannot grow without
// limit. Keys come from request-supplied Stremio content IDs, so an unbounded
// map retains an entry per distinct ID seen for the process lifetime.
func TestSeriesMetadataCacheIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	restore := overrideTVmazeBaseURL(srv.URL)
	defer restore()
	resetSeriesMetadataCaches()

	for i := 0; i < maxSeriesMetadataCacheEntries+50; i++ {
		resolveSeriesTitleAliases(context.Background(), "tt"+strconv.Itoa(i))
	}

	if n := seriesTitleAliasesCacheLen(); n > maxSeriesMetadataCacheEntries {
		t.Errorf("cache holds %d entries, want at most %d", n, maxSeriesMetadataCacheEntries)
	}
}

// overrideTVmazeBaseURL points the resolvers at a test server and returns a
// function restoring the real endpoint.
func overrideTVmazeBaseURL(base string) func() {
	previous := tvmazeBaseURL
	tvmazeBaseURL = base
	return func() { tvmazeBaseURL = previous }
}

// resetSeriesMetadataCaches clears both caches so tests start from empty.
func resetSeriesMetadataCaches() {
	seriesTitleAliasesCache.reset()
	seriesEpisodeMetaCache.reset()
}

func seriesTitleAliasesCacheLen() int { return seriesTitleAliasesCache.len() }
