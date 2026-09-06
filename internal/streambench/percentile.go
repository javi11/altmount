// Package streambench holds the result schema, statistics helpers and the
// regression comparison used by the streaming benchmarks in
// internal/nzbfilesystem and by cmd/compare.
package streambench

import (
	"sort"
	"sync"
	"time"
)

// Samples collects durations from concurrent goroutines and answers
// percentile queries over the sorted set.
type Samples struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *Samples) Add(d time.Duration) {
	s.mu.Lock()
	s.d = append(s.d, d)
	s.mu.Unlock()
}

// P returns the q-quantile (q in [0,1]) using nearest-rank on the sorted
// samples. Zero when there are no samples.
func (s *Samples) P(q float64) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.d)
	if n == 0 {
		return 0
	}
	sort.Slice(s.d, func(i, j int) bool { return s.d[i] < s.d[j] })
	if q <= 0 {
		return s.d[0]
	}
	if q >= 1 {
		return s.d[n-1]
	}
	idx := int(q*float64(n)+0.5) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return s.d[idx]
}

func (s *Samples) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.d)
}

func (s *Samples) Mean() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.d) == 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range s.d {
		sum += d
	}
	return sum / time.Duration(len(s.d))
}
