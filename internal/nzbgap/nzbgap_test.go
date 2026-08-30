package nzbgap

import (
	"strings"
	"testing"

	"github.com/javi11/nzbparser"
)

func file(ids map[int]string, totalSegments int) nzbparser.NzbFile {
	f := nzbparser.NzbFile{TotalSegments: totalSegments}
	for n, id := range ids {
		f.Segments = append(f.Segments, nzbparser.NzbSegment{Number: n, ID: id, Bytes: 700000})
	}
	return f
}

func TestFillFileInsertsPlaceholdersForMissingNumbers(t *testing.T) {
	f := file(map[int]string{1: "a@x", 2: "b@x", 4: "d@x", 5: "e@x"}, 0)

	inserted := FillFile(&f)

	if len(inserted) != 1 {
		t.Fatalf("inserted = %v, want exactly one synthetic id", inserted)
	}
	if len(f.Segments) != 5 {
		t.Fatalf("segments = %d, want 5", len(f.Segments))
	}
	// Sorted by number, placeholder at number 3.
	for i, seg := range f.Segments {
		if seg.Number != i+1 {
			t.Fatalf("segment %d has number %d, want %d", i, seg.Number, i+1)
		}
	}
	if f.Segments[2].ID != inserted[0] {
		t.Fatalf("segment 3 id = %q, want synthetic %q", f.Segments[2].ID, inserted[0])
	}
	if !IsSynthetic(inserted[0]) {
		t.Fatalf("IsSynthetic(%q) = false, want true", inserted[0])
	}
	if IsSynthetic("a@x") {
		t.Fatal("IsSynthetic reports a real id as synthetic")
	}
}

func TestFillFileUsesTotalSegmentsForTailGaps(t *testing.T) {
	f := file(map[int]string{1: "a@x", 2: "b@x"}, 4)

	inserted := FillFile(&f)

	if len(inserted) != 2 {
		t.Fatalf("inserted = %v, want two synthetic ids (numbers 3 and 4)", inserted)
	}
	if len(f.Segments) != 4 {
		t.Fatalf("segments = %d, want 4", len(f.Segments))
	}
}

func TestFillFileCompleteFileUntouched(t *testing.T) {
	f := file(map[int]string{1: "a@x", 2: "b@x", 3: "c@x"}, 3)

	if inserted := FillFile(&f); len(inserted) != 0 {
		t.Fatalf("inserted = %v, want none", inserted)
	}
	if len(f.Segments) != 3 {
		t.Fatalf("segments = %d, want 3", len(f.Segments))
	}
}

func TestFillFileDeterministicIDs(t *testing.T) {
	a := file(map[int]string{1: "a@x", 3: "c@x"}, 0)
	b := file(map[int]string{1: "a@x", 3: "c@x"}, 0)

	ia, ib := FillFile(&a), FillFile(&b)
	if ia[0] != ib[0] {
		t.Fatalf("same file produced different synthetic ids: %q vs %q", ia[0], ib[0])
	}

	// A different file (different anchor id) yields a different id.
	c := file(map[int]string{1: "z@x", 3: "c@x"}, 0)
	if ic := FillFile(&c); ic[0] == ia[0] {
		t.Fatalf("different files share a synthetic id: %q", ic[0])
	}
}

func TestFillFileIdempotent(t *testing.T) {
	f := file(map[int]string{1: "a@x", 3: "c@x"}, 0)

	first := FillFile(&f)
	second := FillFile(&f)

	if len(second) != 0 {
		t.Fatalf("second fill inserted %v, want none", second)
	}
	if len(f.Segments) != 3 {
		t.Fatalf("segments = %d, want 3", len(f.Segments))
	}
	_ = first
}

func TestFillFillsEveryFileAndReportsPerFile(t *testing.T) {
	n := &nzbparser.Nzb{Files: []nzbparser.NzbFile{
		file(map[int]string{1: "a@x", 3: "c@x"}, 0),
		file(map[int]string{1: "p@x", 2: "q@x"}, 2),
	}}
	n.Files[0].Filename = "vol.r43"
	n.Files[1].Filename = "vol.rar"

	gaps := Fill(n)

	if len(gaps) != 1 {
		t.Fatalf("gaps = %v, want one damaged file", gaps)
	}
	if ids := gaps["vol.r43"]; len(ids) != 1 || !IsSynthetic(ids[0]) {
		t.Fatalf("gaps[vol.r43] = %v, want one synthetic id", ids)
	}
}

func TestSyntheticIDLooksLikeMessageID(t *testing.T) {
	id := SyntheticID("real-anchor@news.example.com", 7)
	if !strings.Contains(id, "@") {
		t.Fatalf("synthetic id %q does not look like a message id", id)
	}
	if strings.ContainsAny(id, "<> \t") {
		t.Fatalf("synthetic id %q contains characters unsafe for STAT/patch keys", id)
	}
}
