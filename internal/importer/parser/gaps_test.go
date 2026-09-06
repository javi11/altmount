package parser

import (
	"testing"

	"github.com/javi11/altmount/internal/holes"
	"github.com/javi11/nzbparser"
)

func gapTestFile(numbers []int, total int) nzbparser.NzbFile {
	f := nzbparser.NzbFile{Subject: "x", TotalSegments: total}
	for _, n := range numbers {
		size := 700000
		if n == total || (total == 0 && n == numbers[len(numbers)-1]) {
			size = 12345
		}
		f.Segments = append(f.Segments, nzbparser.NzbSegment{Number: n, Bytes: size, ID: "seg-" + string(rune('a'+n))})
	}
	return f
}

func TestInsertSegmentGapPlaceholdersFillsMiddleGaps(t *testing.T) {
	n := &nzbparser.Nzb{Files: nzbparser.NzbFiles{gapTestFile([]int{1, 2, 4, 5, 7}, 7)}}
	if got := InsertSegmentGapPlaceholders(n); got != 2 {
		t.Fatalf("inserted = %d, want 2", got)
	}
	segs := n.Files[0].Segments
	if len(segs) != 7 {
		t.Fatalf("segments = %d, want 7", len(segs))
	}
	for i, s := range segs {
		if s.Number != i+1 {
			t.Fatalf("segment %d has number %d, want %d (sorted, contiguous)", i, s.Number, i+1)
		}
	}
	for _, idx := range []int{2, 5} {
		s := segs[idx]
		if !holes.IsPlaceholderID(s.ID) {
			t.Fatalf("segment %d id %q is not a placeholder", s.Number, s.ID)
		}
		if s.Bytes != 700000 {
			t.Fatalf("placeholder %d bytes = %d, want the typical part size 700000", s.Number, s.Bytes)
		}
	}
	if holes.IsPlaceholderID(segs[0].ID) || holes.IsPlaceholderID(segs[6].ID) {
		t.Fatal("real segments must keep their ids")
	}
	if segs[2].ID == segs[5].ID {
		t.Fatal("placeholders must have distinct ids")
	}
}

func TestInsertSegmentGapPlaceholdersUsesDeclaredTotalForTailGaps(t *testing.T) {
	n := &nzbparser.Nzb{Files: nzbparser.NzbFiles{gapTestFile([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 12)}}
	if got := InsertSegmentGapPlaceholders(n); got != 2 {
		t.Fatalf("inserted = %d, want 2 tail placeholders", got)
	}
	segs := n.Files[0].Segments
	if len(segs) != 12 || !holes.IsPlaceholderID(segs[10].ID) || !holes.IsPlaceholderID(segs[11].ID) {
		t.Fatalf("tail not filled: %+v", segs)
	}
}

func TestInsertSegmentGapPlaceholdersLeavesCompleteAndHopelessFilesAlone(t *testing.T) {
	complete := gapTestFile([]int{1, 2, 3, 4}, 4)
	// Only 2 of 40 present: past the fraction guard, numbering is not trusted.
	hopeless := gapTestFile([]int{1, 40}, 40)
	unnumbered := nzbparser.NzbFile{Segments: nzbparser.NzbSegments{{Number: 0, Bytes: 1, ID: "a"}, {Number: 0, Bytes: 1, ID: "b"}}}
	n := &nzbparser.Nzb{Files: nzbparser.NzbFiles{complete, hopeless, unnumbered}}
	if got := InsertSegmentGapPlaceholders(n); got != 0 {
		t.Fatalf("inserted = %d, want 0", got)
	}
	if len(n.Files[0].Segments) != 4 || len(n.Files[1].Segments) != 2 || len(n.Files[2].Segments) != 2 {
		t.Fatalf("segment lists changed: %d %d %d", len(n.Files[0].Segments), len(n.Files[1].Segments), len(n.Files[2].Segments))
	}
	if InsertSegmentGapPlaceholders(nil) != 0 {
		t.Fatal("nil nzb must be a no-op")
	}
}

func TestShouldSkipFirstSegmentFetchSevenZipContinuationVolumes(t *testing.T) {
	uniform := nzbparser.NzbSegments{{Number: 1, Bytes: 700000, ID: "a"}, {Number: 2, Bytes: 700000, ID: "b"}, {Number: 3, Bytes: 12345, ID: "c"}}
	cases := []struct {
		name string
		want bool
	}{
		{"Movie.2024.1080p.7z.002", true},
		{"Movie.2024.1080p.7z.017", true},
		{"Movie.2024.1080p.7z.001", false}, // the head volume is read
		{"Movie.2024.1080p.7z", false},
		{"Movie.2024.1080p.part02.rar", false}, // every RAR volume's header is read
		{"Movie.2024.1080p.mkv", true},
		{"Movie.2024.1080p.vol063+64.par2", false}, // 3 segments: could be an index, keep the fetch
	}
	for _, c := range cases {
		f := &nzbparser.NzbFile{Filename: c.name, Segments: uniform}
		if got := shouldSkipFirstSegmentFetch(f); got != c.want {
			t.Errorf("shouldSkipFirstSegmentFetch(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestShouldSkipFirstSegmentFetchPar2RecoveryVolumes(t *testing.T) {
	segs := make(nzbparser.NzbSegments, 0, 40)
	for i := 1; i <= 40; i++ {
		size := 700000
		if i == 40 {
			size = 1000
		}
		segs = append(segs, nzbparser.NzbSegment{Number: i, Bytes: size, ID: "p" + string(rune('a'+i%26))})
	}
	f := &nzbparser.NzbFile{Filename: "Movie.2024.1080p.vol063+64.par2", Segments: segs}
	if !shouldSkipFirstSegmentFetch(f) {
		t.Fatal("a 40-segment .par2 is a recovery volume whose bytes are never read; its first segment must be skipped")
	}
	f.Filename = "Movie.2024.1080p.par2"
	f.Segments = segs[:3]
	if shouldSkipFirstSegmentFetch(f) {
		t.Fatal("a small .par2 may be the index; it must be fetched")
	}
}
