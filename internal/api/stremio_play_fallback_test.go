package api

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/prowlarr"
)

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

func urlEscape(s string) string { return url.QueryEscape(s) }

// failedItem builds a failed Stremio queue item for exclusion tests.
func failedItem(nzbPath string, updatedAt time.Time) *database.ImportQueueItem {
	return &database.ImportQueueItem{NzbPath: nzbPath, UpdatedAt: updatedAt}
}

func resultsFromTitles(titles ...string) []prowlarr.NZBResult {
	results := make([]prowlarr.NZBResult, len(titles))
	for i, ti := range titles {
		results[i] = prowlarr.NZBResult{Title: ti, DownloadURL: "https://prowlarr/dl", Size: 1e9}
	}
	return results
}

func titlesOf(results []prowlarr.NZBResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Title
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFilterStremioResults_ExcludesFailed(t *testing.T) {
	tests := []struct {
		name      string
		titles    []string
		failedKey string
		languages []string
		qualities []string
		want      []string
	}{
		{
			name:   "no failures keeps everything",
			titles: []string{"Alpha 2024 1080p", "Bravo 2024 1080p"},
			want:   []string{"Alpha 2024 1080p", "Bravo 2024 1080p"},
		},
		{
			name:      "failed release is dropped",
			titles:    []string{"Alpha 2024 1080p", "Bravo 2024 1080p"},
			failedKey: sanitizeFilename("Alpha 2024 1080p"),
			want:      []string{"Bravo 2024 1080p"},
		},
		{
			name:      "all failed yields empty",
			titles:    []string{"Alpha 2024 1080p"},
			failedKey: sanitizeFilename("Alpha 2024 1080p"),
			want:      nil,
		},
		{
			name:      "title needing sanitization still matches",
			titles:    []string{"Movie: The Sequel 2024 1080p"},
			failedKey: sanitizeFilename("Movie: The Sequel 2024 1080p"),
			want:      nil,
		},
		{
			name:      "release already dropped by quality filter does not double-drop",
			titles:    []string{"Alpha 2024 720p", "Bravo 2024 1080p"},
			failedKey: sanitizeFilename("Alpha 2024 720p"),
			qualities: []string{"1080p"},
			want:      []string{"Bravo 2024 1080p"},
		},
		{
			name:      "language filter still applies alongside exclusion",
			titles:    []string{"Alpha 2024 1080p Spanish", "Bravo 2024 1080p"},
			languages: []string{"Spanish"},
			want:      []string{"Alpha 2024 1080p Spanish"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isFailed := func(safeTitle string) bool {
				return tt.failedKey != "" && safeTitle == tt.failedKey
			}

			got := filterStremioResults(resultsFromTitles(tt.titles...), tt.languages, tt.qualities, nil, isFailed)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v; want %v", titlesOf(got), tt.want)
			}
			if !equalStrings(titlesOf(got), tt.want) {
				t.Errorf("got %v; want %v", titlesOf(got), tt.want)
			}
		})
	}
}

func TestFilterStremioResults_NilPredicate(t *testing.T) {
	got := filterStremioResults(resultsFromTitles("Alpha 2024 1080p"), nil, nil, nil, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 result with nil predicate, got %d", len(got))
	}
}

func TestStremioFailedPredicate(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	title := "Some Movie 2024 1080p WEB-DL"
	key := sanitizeFilename(title)
	path := "/config/.nzbs/Movies/7/" + key + ".nzb"

	tests := []struct {
		name     string
		failed   []*database.ImportQueueItem
		memKeys  map[string]struct{}
		ttlHours int
		want     bool
	}{
		{
			name:     "failed within ttl is excluded",
			failed:   []*database.ImportQueueItem{failedItem(path, now.Add(-1 * time.Hour))},
			ttlHours: 24,
			want:     true,
		},
		{
			name:     "failed past ttl is not excluded",
			failed:   []*database.ImportQueueItem{failedItem(path, now.Add(-48 * time.Hour))},
			ttlHours: 24,
			want:     false,
		},
		{
			name:     "ttl zero excludes regardless of age",
			failed:   []*database.ImportQueueItem{failedItem(path, now.Add(-1000 * time.Hour))},
			ttlHours: 0,
			want:     true,
		},
		{
			name:     "unrelated failed path does not match",
			failed:   []*database.ImportQueueItem{failedItem("/config/.nzbs/Movies/9/Other Release.nzb", now)},
			ttlHours: 24,
			want:     false,
		},
		{
			name:     "in-memory key excludes without a queue row",
			memKeys:  map[string]struct{}{key: {}},
			ttlHours: 24,
			want:     true,
		},
		{
			name:     "empty everything does not exclude",
			ttlHours: 24,
			want:     false,
		},
		{
			name:     "windows-style backslash path still matches",
			failed:   []*database.ImportQueueItem{failedItem(`C:\config\.nzbs\Movies\7\`+key+".nzb", now)},
			ttlHours: 24,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isFailed := stremioFailedPredicate(tt.failed, tt.memKeys, tt.ttlHours, now)
			if got := isFailed(key); got != tt.want {
				t.Errorf("isFailed(%q) = %v; want %v", key, got, tt.want)
			}
		})
	}
}

func TestOrderStremioResults_CachedFirstStable(t *testing.T) {
	results := resultsFromTitles("Alpha 2024", "Bravo 2024", "Charlie 2024", "Delta 2024")
	cachedKeys := map[string]struct{}{
		sanitizeFilename("Bravo 2024"): {},
		sanitizeFilename("Delta 2024"): {},
	}
	isCached := func(safeTitle string) bool {
		_, ok := cachedKeys[safeTitle]
		return ok
	}

	got := titlesOf(orderStremioResults(results, isCached, config.ProwlarrConfig{}))
	want := []string{"Bravo 2024", "Delta 2024", "Alpha 2024", "Charlie 2024"}
	if !equalStrings(got, want) {
		t.Errorf("got %v; want %v", got, want)
	}

	// Ordering must not mutate the caller's slice: the stream list and the fallback
	// chain both order the same search results.
	if titlesOf(results)[0] != "Alpha 2024" {
		t.Errorf("input slice was mutated: %v", titlesOf(results))
	}
}

func TestNextStremioCandidate(t *testing.T) {
	cands := stremioCandidates(resultsFromTitles("Alpha", "Bravo", "Charlie"), "tt1")

	tests := []struct {
		name       string
		cands      []stremioPlayCandidate
		current    string
		startAfter int
		wantIdx    int
		wantOK     bool
	}{
		{
			name:       "advances from explicit index",
			cands:      cands,
			startAfter: 0,
			wantIdx:    1,
			wantOK:     true,
		},
		{
			name:       "exhausted at last index",
			cands:      cands,
			startAfter: 2,
			wantOK:     false,
		},
		{
			name:       "finds current by title on first advance",
			cands:      cands,
			current:    sanitizeFilename("Alpha"),
			startAfter: -1,
			wantIdx:    1,
			wantOK:     true,
		},
		{
			name:       "current is last known release",
			cands:      cands,
			current:    sanitizeFilename("Charlie"),
			startAfter: -1,
			wantOK:     false,
		},
		{
			name:       "current absent starts from the top",
			cands:      cands,
			current:    "Release That Was Filtered Out",
			startAfter: -1,
			wantIdx:    0,
			wantOK:     true,
		},
		{
			name:       "empty candidate list is exhausted",
			cands:      nil,
			startAfter: -1,
			wantOK:     false,
		},
		{
			name:       "single-element list matching current is exhausted",
			cands:      stremioCandidates(resultsFromTitles("Solo"), "tt1"),
			current:    sanitizeFilename("Solo"),
			startAfter: -1,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, ok := nextStremioCandidate(tt.cands, tt.current, tt.startAfter)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v; want %v", ok, tt.wantOK)
			}
			if ok && idx != tt.wantIdx {
				t.Errorf("idx = %d; want %d", idx, tt.wantIdx)
			}
		})
	}
}

// TestStremioCandidatesMatchStreamEntries pins the invariant that makes fallback
// correct: the candidate list and the stream list must agree on both ordering and
// release keys. If they drift, /play advances to a release the user never saw, or
// re-tries the one that just failed.
func TestStremioCandidatesMatchStreamEntries(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	results := resultsFromTitles("Alpha 2024 1080p", "Bravo: The Sequel 2024 1080p", "Charlie 2024 1080p")

	// Bravo is cached, so both paths must float it to the front.
	cached := []*database.ImportQueueItem{
		cachedItem("/nzbs/"+sanitizeFilename("Bravo: The Sequel 2024 1080p")+".nzb", "/s/b",
			timePtr(now.Add(-time.Hour))),
	}

	isCached := stremioCachedPredicate(cached, 24, now)
	ordered := orderStremioResults(results, isCached, config.ProwlarrConfig{})
	cands := stremioCandidates(ordered, "tt1")

	entries := buildStremioStreamEntries(results, cached, 24, now,
		"https://host", "k", "movie", 0, 0, "tt1", config.ProwlarrConfig{})

	if len(cands) != len(entries) {
		t.Fatalf("candidates (%d) and entries (%d) differ in length", len(cands), len(entries))
	}
	if !entries[0].Cached {
		t.Error("expected the cached release to sort first")
	}

	for i := range cands {
		// The play URL embeds the release key as its title param, so a matching
		// title= proves both paths derived the same key for the same position.
		if !contains(entries[i].URL, "title="+urlEscape(cands[i].SafeTitle)) {
			t.Errorf("position %d: entry URL %q does not carry candidate key %q",
				i, entries[i].URL, cands[i].SafeTitle)
		}
	}
}

func TestOrderStremioResults_CustomScoresAndExcludes(t *testing.T) {
	results := resultsFromTitles(
		"Movie.2024.1080p.WEB-DL.DV",
		"Movie.2024.2160p.UHD.REMUX",
		"Movie.2024.1080p.WEB-DL",
		"Movie.2024.CAM",
	)

	prowlarrCfg := config.ProwlarrConfig{
		ExcludeKeywords: []string{"CAM"},
		CustomScores: map[string]int{
			"REMUX": 500,
			"1080p": 200,
			"DV":    -1000,
		},
	}

	filtered := filterStremioResults(results, nil, nil, prowlarrCfg.ExcludeKeywords, nil)
	if len(filtered) != 3 {
		t.Fatalf("expected 3 results after excluding CAM, got %d", len(filtered))
	}

	ordered := orderStremioResults(filtered, func(string) bool { return false }, prowlarrCfg)
	titles := titlesOf(ordered)

	// Expected rank:
	// 1. Movie.2024.2160p.UHD.REMUX (REMUX score +500)
	// 2. Movie.2024.1080p.WEB-DL (1080p score +200)
	// 3. Movie.2024.1080p.WEB-DL.DV (1080p +200, DV -1000 = -800)
	want := []string{
		"Movie.2024.2160p.UHD.REMUX",
		"Movie.2024.1080p.WEB-DL",
		"Movie.2024.1080p.WEB-DL.DV",
	}
	if !equalStrings(titles, want) {
		t.Errorf("got %v; want %v", titles, want)
	}
}
