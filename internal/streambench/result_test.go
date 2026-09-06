package streambench

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSamplesPercentiles(t *testing.T) {
	var s Samples
	for i := 1; i <= 100; i++ {
		s.Add(time.Duration(i) * time.Millisecond)
	}
	if got := s.P(0.5); got != 50*time.Millisecond {
		t.Fatalf("p50 = %v, want 50ms", got)
	}
	if got := s.P(0.99); got != 99*time.Millisecond {
		t.Fatalf("p99 = %v, want 99ms", got)
	}
	if got := s.P(1); got != 100*time.Millisecond {
		t.Fatalf("p100 = %v, want 100ms", got)
	}
	if s.Count() != 100 {
		t.Fatalf("count = %d", s.Count())
	}
}

func TestSamplesEmpty(t *testing.T) {
	var s Samples
	if s.P(0.5) != 0 || s.Mean() != 0 {
		t.Fatal("empty samples must report zero")
	}
}

func TestResultRoundTrip(t *testing.T) {
	r := &Result{GitSHA: "abc123", Profile: "premium-750k"}
	r.Add("B1", Metric{Name: "ttfb_p50", Unit: "ms", Value: 12.5})
	r.Add("B1", Metric{Name: "articles", Unit: "count", Value: 3})
	r.Add("B2", Metric{Name: "throughput", Unit: "MB/s", Value: 40, HigherIsBetter: true})

	path := filepath.Join(t.TempDir(), "r.json")
	if err := Save(path, r); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Scenarios) != 2 || len(got.Scenarios[0].Metrics) != 2 {
		t.Fatalf("unexpected shape: %+v", got.Scenarios)
	}
	if !got.Scenarios[1].Metrics[0].HigherIsBetter {
		t.Fatal("HigherIsBetter lost in round trip")
	}
}
