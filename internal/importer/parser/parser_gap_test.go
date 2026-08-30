package parser

import (
	"context"
	"fmt"
	"testing"

	"github.com/javi11/altmount/internal/nzbgap"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/nzbparser"
)

// gapSegs builds n segments with distinct IDs; indexes in synthetic get a
// placeholder ID as nzbgap.FillFile would insert.
func gapSegs(n int, synthetic ...int) nzbparser.NzbSegments {
	syn := map[int]bool{}
	for _, i := range synthetic {
		syn[i] = true
	}
	segs := make(nzbparser.NzbSegments, n)
	for i := range segs {
		id := fmt.Sprintf("real-%d@test", i)
		if syn[i] {
			id = nzbgap.SyntheticID("real-0@test", i+1)
		}
		segs[i] = nzbparser.NzbSegment{Bytes: 716800, Number: i + 1, ID: id}
	}
	return segs
}

// Normalization must never probe a synthetic gap placeholder for the standard
// part size — no provider has it, and the pointless 430 would skip the file.
// It must probe the next real middle segment instead.
func TestNormalizeSegmentSizes_SkipsSyntheticMiddleProbe(t *testing.T) {
	fp := fakepool.New()
	mgr := newFakeFullPoolManager(fp)
	p := NewParser(mgr, stormConfigGetter(4))

	segs := gapSegs(4, 1) // segment index 1 (number 2) is a gap placeholder
	first := firstSegmentYencInfo{PartSize: 700000, FileSize: 700000*3 + 120000}

	// standardPartSize unknown -> normalization needs a middle-segment probe.
	_ = p.normalizeSegmentSizesWithYenc(context.Background(), segs, first, 0, nil)

	if got := fp.PerMessageCalls(segs[1].ID); got != 0 {
		t.Fatalf("synthetic placeholder was probed %d times, want 0", got)
	}
	if got := fp.PerMessageCalls("real-2@test"); got == 0 {
		t.Fatal("expected the probe to fall through to the next real middle segment")
	}
}

// The NZB-wide representative middle-segment pick must skip synthetic
// placeholders for the same reason.
func TestPickRepresentativeMiddleSegmentSkipsSynthetic(t *testing.T) {
	segs := gapSegs(4, 1)
	cache := []*FirstSegmentData{{
		File: &nzbparser.NzbFile{Segments: segs},
	}}

	seg, _, ok := pickRepresentativeMiddleSegment(cache, nil)
	if !ok {
		t.Fatal("no representative picked")
	}
	if nzbgap.IsSynthetic(seg.ID) {
		t.Fatalf("picked synthetic placeholder %q as representative", seg.ID)
	}
}
