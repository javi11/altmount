package failures

import "testing"

func TestTracker_BumpAndReset(t *testing.T) {
	tr := NewTracker()

	if got := tr.Bump("k"); got != 1 {
		t.Fatalf("first bump = %d, want 1", got)
	}
	if got := tr.Bump("k"); got != 2 {
		t.Fatalf("second bump = %d, want 2", got)
	}
	if got := tr.Bump("other"); got != 1 {
		t.Fatalf("independent key bump = %d, want 1", got)
	}

	tr.Reset("k")
	if got := tr.Bump("k"); got != 1 {
		t.Fatalf("bump after reset = %d, want 1", got)
	}
}

func TestTracker_NilAndEmptySafe(t *testing.T) {
	var tr *Tracker
	if got := tr.Bump("k"); got != 0 {
		t.Fatalf("nil tracker bump = %d, want 0", got)
	}
	tr.Reset("k") // must not panic

	real := NewTracker()
	if got := real.Bump(""); got != 0 {
		t.Fatalf("empty key bump = %d, want 0", got)
	}
}

func TestKeyBuilders_MatchWorkerFormats(t *testing.T) {
	// These formats are shared state between the queue-cleanup worker and the
	// scanner — changing them silently splits the combined failure counts.
	cases := map[string]string{
		EpisodeKey("inst", 42): "sonarr|inst|ep:42",
		MovieKey("inst", 7):    "radarr|inst|movie:7",
		AlbumKey("inst", 3):    "lidarr|inst|album:3",
		BookKey("inst", 9):     "readarr|inst|book:9",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("key = %q, want %q", got, want)
		}
	}
}
