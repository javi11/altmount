package holes

import (
	"reflect"
	"testing"
)

func TestClassify(t *testing.T) {
	const segBytes = 768 * 1024

	tests := []struct {
		name      string
		runs      []Run
		fileBytes int64
		want      Verdict
	}{
		{
			name: "no runs is clean",
			want: VerdictClean,
		},
		{
			name:      "single short run is degraded",
			runs:      []Run{{Start: 100, Count: 2}},
			fileBytes: 4 << 30,
			want:      VerdictDegraded,
		},
		{
			name:      "run at the pad cap is degraded",
			runs:      []Run{{Start: 100, Count: MaxPadRunSegments}},
			fileBytes: 4 << 30,
			want:      VerdictDegraded,
		},
		{
			name:      "run beyond the pad cap fails",
			runs:      []Run{{Start: 100, Count: MaxPadRunSegments + 1}},
			fileBytes: 4 << 30,
			want:      VerdictFailed,
		},
		{
			name: "total at the cumulative cap is degraded",
			runs: func() []Run {
				var rs []Run
				for i := range MaxPadTotalSegments {
					rs = append(rs, Run{Start: i * 10, Count: 1})
				}
				return rs
			}(),
			fileBytes: 40 << 30,
			want:      VerdictDegraded,
		},
		{
			name: "total beyond the cumulative cap fails",
			runs: func() []Run {
				var rs []Run
				for i := range MaxPadTotalSegments + 1 {
					rs = append(rs, Run{Start: i * 10, Count: 1})
				}
				return rs
			}(),
			fileBytes: 40 << 30,
			want:      VerdictFailed,
		},
		{
			name:      "small file fails on the bytes ratio",
			runs:      []Run{{Start: 3, Count: 2}},
			fileBytes: 20 * segBytes, // 2 segments = 10% > 2%
			want:      VerdictFailed,
		},
		{
			name:      "unknown file size skips the ratio guard",
			runs:      []Run{{Start: 3, Count: 2}},
			fileBytes: 0,
			want:      VerdictDegraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.runs, tt.fileBytes, segBytes); got != tt.want {
				t.Errorf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyProjected(t *testing.T) {
	tests := []struct {
		name          string
		hits, sampled int
		total         int
		longest       int
		want          Verdict
	}{
		{
			name: "no hits is unknown, never clean",
			hits: 0, sampled: 50, total: 1000, longest: 0,
			want: VerdictUnknown,
		},
		{
			name: "measured long run fails regardless of hit count",
			hits: 5, sampled: 50, total: 1000, longest: MaxPadRunSegments + 1,
			want: VerdictFailed,
		},
		{
			name: "few hits stay degraded even at a high rate",
			hits: ProjectionMinHits - 1, sampled: 10, total: 100000, longest: 1,
			want: VerdictDegraded,
		},
		{
			name: "enough hits projecting past the margin fails",
			// 10/50 sampled = 20% of 10000 segments = 2000 projected > 2*64.
			hits: 10, sampled: 50, total: 10000, longest: 1,
			want: VerdictFailed,
		},
		{
			name: "enough hits but small projection is degraded",
			// 8/500 = 1.6% of 5000 = 80 projected < 128.
			hits: 8, sampled: 500, total: 5000, longest: 1,
			want: VerdictDegraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyProjected(tt.hits, tt.sampled, tt.total, tt.longest); got != tt.want {
				t.Errorf("ClassifyProjected() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAccumulatorMerging(t *testing.T) {
	tests := []struct {
		name        string
		add         []Run
		wantRuns    []Run
		wantTotal   int
		wantLongest int
	}{
		{
			name:        "single add",
			add:         []Run{{Start: 5, Count: 1}},
			wantRuns:    []Run{{Start: 5, Count: 1}},
			wantTotal:   1,
			wantLongest: 1,
		},
		{
			name:        "adjacent segments merge into one run",
			add:         []Run{{Start: 5, Count: 1}, {Start: 6, Count: 1}, {Start: 7, Count: 1}},
			wantRuns:    []Run{{Start: 5, Count: 3}},
			wantTotal:   3,
			wantLongest: 3,
		},
		{
			name:        "out-of-order adjacent segments merge",
			add:         []Run{{Start: 7, Count: 1}, {Start: 5, Count: 1}, {Start: 6, Count: 1}},
			wantRuns:    []Run{{Start: 5, Count: 3}},
			wantTotal:   3,
			wantLongest: 3,
		},
		{
			name:        "disjoint runs stay separate and sorted",
			add:         []Run{{Start: 20, Count: 2}, {Start: 5, Count: 1}},
			wantRuns:    []Run{{Start: 5, Count: 1}, {Start: 20, Count: 2}},
			wantTotal:   3,
			wantLongest: 2,
		},
		{
			name:        "bridging run merges its neighbors",
			add:         []Run{{Start: 10, Count: 2}, {Start: 20, Count: 2}, {Start: 12, Count: 9}},
			wantRuns:    []Run{{Start: 10, Count: 12}},
			wantTotal:   12,
			wantLongest: 12,
		},
		{
			name:        "overlap does not double count",
			add:         []Run{{Start: 10, Count: 5}, {Start: 12, Count: 5}},
			wantRuns:    []Run{{Start: 10, Count: 7}},
			wantTotal:   7,
			wantLongest: 7,
		},
		{
			name:        "duplicate add is idempotent",
			add:         []Run{{Start: 10, Count: 3}, {Start: 10, Count: 3}},
			wantRuns:    []Run{{Start: 10, Count: 3}},
			wantTotal:   3,
			wantLongest: 3,
		},
		{
			name:        "zero or negative count ignored",
			add:         []Run{{Start: 10, Count: 0}, {Start: 12, Count: -2}},
			wantRuns:    []Run{},
			wantTotal:   0,
			wantLongest: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc Accumulator
			for _, r := range tt.add {
				acc.AddRun(r)
			}
			got := acc.Runs()
			if len(got) == 0 && len(tt.wantRuns) == 0 {
				// treat nil and empty as equal
			} else if !reflect.DeepEqual(got, tt.wantRuns) {
				t.Errorf("Runs() = %v, want %v", got, tt.wantRuns)
			}
			if acc.Total() != tt.wantTotal {
				t.Errorf("Total() = %d, want %d", acc.Total(), tt.wantTotal)
			}
			if acc.LongestRun() != tt.wantLongest {
				t.Errorf("LongestRun() = %d, want %d", acc.LongestRun(), tt.wantLongest)
			}
		})
	}
}

func TestAccumulatorHasAndLoad(t *testing.T) {
	var acc Accumulator
	acc.Load([]Run{{Start: 5, Count: 3}, {Start: 20, Count: 1}})

	for _, idx := range []int{5, 6, 7, 20} {
		if !acc.Has(idx) {
			t.Errorf("Has(%d) = false, want true", idx)
		}
	}
	for _, idx := range []int{4, 8, 19, 21} {
		if acc.Has(idx) {
			t.Errorf("Has(%d) = true, want false", idx)
		}
	}
}

func TestEligibleFile(t *testing.T) {
	eligible := []string{"Movie.2024.mkv", "clip.MP4", "a/b/show.webm", "x.m4v", "y.mov"}
	for _, name := range eligible {
		if !EligibleFile(name) {
			t.Errorf("EligibleFile(%q) = false, want true", name)
		}
	}
	ineligible := []string{"archive.rar", "sub.srt", "movie.avi", "noext", "data.bin", "x.mkv.par2"}
	for _, name := range ineligible {
		if EligibleFile(name) {
			t.Errorf("EligibleFile(%q) = true, want false", name)
		}
	}
}
