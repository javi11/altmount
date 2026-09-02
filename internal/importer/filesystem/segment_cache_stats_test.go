package filesystem

import (
	"fmt"
	"testing"
)

// The cache was shipped without any way to tell whether it earns its memory.
// These counters exist so a real import can answer that from its logs rather
// than from a model of how archive analysis is assumed to read.
func TestImportSegmentCache_StatsCountHitsMissesEvictions(t *testing.T) {
	const seg = 1 << 20
	c := NewImportSegmentCache(2 * seg)

	if got := c.Stats(); got.Hits != 0 || got.Misses != 0 || got.Evictions != 0 {
		t.Fatalf("fresh cache has non-zero stats: %+v", got)
	}

	// Miss, then store.
	if _, ok := c.Get("a"); ok {
		t.Fatal("empty cache should not hit")
	}
	if err := c.Put("a", make([]byte, seg)); err != nil {
		t.Fatal(err)
	}

	// Hit.
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should hit")
	}

	// Force an eviction: two more entries against a 2-entry bound.
	if err := c.Put("b", make([]byte, seg)); err != nil {
		t.Fatal(err)
	}
	if err := c.Put("c", make([]byte, seg)); err != nil {
		t.Fatal(err)
	}

	got := c.Stats()
	if got.Hits != 1 {
		t.Errorf("Hits = %d, want 1", got.Hits)
	}
	if got.Misses != 1 {
		t.Errorf("Misses = %d, want 1", got.Misses)
	}
	if got.Evictions == 0 {
		t.Error("Evictions = 0, want at least 1 after exceeding the bound")
	}
}

// A hit rate is the number an operator actually reads, so the type should
// compute it rather than making every caller redo the division — including
// the zero-request case, which must not divide by zero.
func TestImportSegmentCacheStats_HitRate(t *testing.T) {
	tests := []struct {
		name       string
		hits, miss int64
		want       float64
	}{
		{"no requests", 0, 0, 0},
		{"all hits", 3, 0, 1},
		{"all misses", 0, 4, 0},
		{"three quarters", 3, 1, 0.75},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ImportSegmentCacheStats{Hits: tt.hits, Misses: tt.miss}
			if got := s.HitRate(); got != tt.want {
				t.Errorf("HitRate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestImportSegmentCache_StatsAreConcurrencySafe(t *testing.T) {
	c := NewImportSegmentCache(1 << 20)
	done := make(chan struct{})
	for w := range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := range 100 {
				id := fmt.Sprintf("k%d", (w*100+i)%20)
				if _, ok := c.Get(id); !ok {
					_ = c.Put(id, make([]byte, 1024))
				}
				_ = c.Stats()
			}
		}()
	}
	for range 8 {
		<-done
	}
	s := c.Stats()
	if s.Hits+s.Misses == 0 {
		t.Error("expected the concurrent workers to have recorded requests")
	}
}
