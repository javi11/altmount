package api

import (
	"strings"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/prowlarr"
)

func timePtr(t time.Time) *time.Time { return &t }

// cachedItem builds a completed Stremio queue item for cache-lookup tests.
func cachedItem(nzbPath, storagePath string, completedAt *time.Time) *database.ImportQueueItem {
	item := &database.ImportQueueItem{
		NzbPath:     nzbPath,
		CompletedAt: completedAt,
	}
	if storagePath != "" {
		item.StoragePath = strPtr(storagePath)
	}
	return item
}

func TestBuildStremioStreamEntries_CacheDetection(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	title := "Some Movie 2024 1080p WEB-DL"
	// safeFilename the play handler would use for this title.
	safeFilename := sanitizeFilename(title) + ".nzb"
	matchingPath := "/config/.nzbs/Movies/7/" + safeFilename

	tests := []struct {
		name        string
		searchTitle string
		cached      []*database.ImportQueueItem
		ttlHours    int
		wantCached  bool
	}{
		{
			name:       "cached within TTL",
			cached:     []*database.ImportQueueItem{cachedItem(matchingPath, "/storage/movie", timePtr(now.Add(-1*time.Hour)))},
			ttlHours:   24,
			wantCached: true,
		},
		{
			name:       "cached but expired",
			cached:     []*database.ImportQueueItem{cachedItem(matchingPath, "/storage/movie", timePtr(now.Add(-48*time.Hour)))},
			ttlHours:   24,
			wantCached: false,
		},
		{
			name:       "ttl disabled caches regardless of age",
			cached:     []*database.ImportQueueItem{cachedItem(matchingPath, "/storage/movie", timePtr(now.Add(-1000*time.Hour)))},
			ttlHours:   0,
			wantCached: true,
		},
		{
			name:       "completed item without storage path is not cached",
			cached:     []*database.ImportQueueItem{cachedItem(matchingPath, "", timePtr(now.Add(-1*time.Hour)))},
			ttlHours:   24,
			wantCached: false,
		},
		{
			name:       "non-matching path is not cached",
			cached:     []*database.ImportQueueItem{cachedItem("/config/.nzbs/Movies/9/Different Release.nzb", "/storage/other", timePtr(now.Add(-1*time.Hour)))},
			ttlHours:   24,
			wantCached: false,
		},
		{
			name:       "empty cache is not cached",
			cached:     nil,
			ttlHours:   24,
			wantCached: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sTitle := title
			if tt.searchTitle != "" {
				sTitle = tt.searchTitle
			}
			results := []prowlarr.NZBResult{{Title: sTitle, DownloadURL: "https://prowlarr/dl/1", Size: 1_500_000_000, Indexer: "IdxA"}}

			entries := buildStremioStreamEntries(results, tt.cached, tt.ttlHours, now,
				"https://host", "thekey", "movie", 0, 0, "tt123", config.ProwlarrConfig{})

			if len(entries) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(entries))
			}
			got := entries[0]

			if got.Cached != tt.wantCached {
				t.Errorf("Cached = %v; want %v", got.Cached, tt.wantCached)
			}
			hasBadge := strings.Contains(got.Name, "⚡ Cached")
			if hasBadge != tt.wantCached {
				t.Errorf("badge present = %v; want %v (name=%q)", hasBadge, tt.wantCached, got.Name)
			}
			// URL always routes through /play regardless of cache status.
			if !strings.Contains(got.URL, "/stremio/thekey/play") {
				t.Errorf("URL does not route through /play: %q", got.URL)
			}
			if !strings.Contains(got.URL, "type=movie") {
				t.Errorf("URL missing type param: %q", got.URL)
			}
		})
	}
}

func TestBuildStremioStreamEntries_CachedSortedFirstStable(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	titles := []string{"Alpha 2024 1080p", "Bravo 2024 1080p", "Charlie 2024 1080p", "Delta 2024 1080p"}
	results := make([]prowlarr.NZBResult, len(titles))
	for i, ti := range titles {
		results[i] = prowlarr.NZBResult{Title: ti, DownloadURL: "https://prowlarr/dl", Size: 1e9}
	}

	// Bravo and Delta are cached; Alpha and Charlie are not.
	cached := []*database.ImportQueueItem{
		cachedItem("/nzbs/"+sanitizeFilename("Bravo 2024 1080p")+".nzb", "/s/b", timePtr(now.Add(-time.Hour))),
		cachedItem("/nzbs/"+sanitizeFilename("Delta 2024 1080p")+".nzb", "/s/d", timePtr(now.Add(-time.Hour))),
	}

	entries := buildStremioStreamEntries(results, cached, 24, now,
		"https://host", "k", "movie", 0, 0, "tt123", config.ProwlarrConfig{})

	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	// Expected order: cached group first in original order (Bravo, Delta),
	// then uncached in original order (Alpha, Charlie).
	wantTitles := []string{"Bravo", "Delta", "Alpha", "Charlie"}
	for i, want := range wantTitles {
		if !strings.Contains(entries[i].Name, want) {
			t.Errorf("position %d: name %q does not contain %q", i, entries[i].Name, want)
		}
	}
	if !entries[0].Cached || !entries[1].Cached {
		t.Errorf("first two entries should be cached")
	}
	if entries[2].Cached || entries[3].Cached {
		t.Errorf("last two entries should not be cached")
	}
}

func TestFormatLibraryStreamNameAndTitle(t *testing.T) {
	libPath := "/library/movies/Sample Movie (2026)/Sample Movie (2026) - [Bluray-2160p][TrueHD Atmos 7.1][DV HDR10][x265]-GROUP.mkv"
	h := &database.FileHealth{
		FilePath:    "complete/movies/Sample.Movie.2026.2160p/sample.mkv",
		LibraryPath: &libPath,
	}

	name := formatLibraryStreamName(h)
	if !strings.Contains(name, "⚡ AltMount Library") || !strings.Contains(name, "2160p") {
		t.Errorf("unexpected library stream name: %q", name)
	}

	title := formatLibraryStreamTitle(h)
	if !strings.Contains(title, "Sample Movie (2026)") || !strings.Contains(title, "Local Library") {
		t.Errorf("unexpected library stream title: %q", title)
	}
}
