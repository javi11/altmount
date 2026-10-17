package par2repair

import (
	"bytes"
	"context"
	"testing"

	"github.com/javi11/nzbparser"

	"github.com/kipsilabs/altmount/internal/holes"
)

// dropSegment removes the segment with the given number from the named NZB
// file, the way an NZB that never listed an article looks.
func dropSegment(t *testing.T, n *nzbparser.Nzb, filename string, number int) {
	t.Helper()
	for i := range n.Files {
		if n.Files[i].Filename != filename {
			continue
		}
		var kept nzbparser.NzbSegments
		for _, s := range n.Files[i].Segments {
			if s.Number != number {
				kept = append(kept, s)
			}
		}
		if len(kept) == len(n.Files[i].Segments) {
			t.Fatalf("%s has no segment %d", filename, number)
		}
		n.Files[i].Segments = kept
		return
	}
	t.Fatalf("no NZB file %q", filename)
}

// An NZB that omits a segment number is what the importer parks with a gap
// placeholder ("altmount-gap-..."). Planning straight from that NZB must lay
// the gap out as a dead article under the same placeholder id, so PAR2
// rebuilds it and the patch lands where the import and streaming paths look
// it up. Previously the volume read as one article short of its PAR2 length
// and the plan was rejected ("part size ... inconsistent").
func TestResolveFromNzbRepairsSegmentMissingFromNzb(t *testing.T) {
	n, fetch, contents, _ := mkEncodedNzbFixture(t, 1<<20, false)
	// a.rar is posted as articles a.rar-0..3 (numbers 1..4); the NZB loses
	// number 2, so the article is not just dead but absent.
	dropSegment(t, n, "a.rar", 2)
	gapID := holes.PlaceholderID(2, "a.rar-0@test")

	res, err := ResolveFromNzb(context.Background(), n, []string{gapID}, fetch,
		Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20}, testLogger(), nil)
	if err != nil {
		t.Fatalf("plan rejected for a release whose NZB omits one segment: %v", err)
	}
	if len(res.Plan.DeadArticles) != 1 || res.Plan.DeadArticles[0].MessageID != gapID {
		t.Fatalf("dead articles = %+v, want only the gap placeholder %s", res.Plan.DeadArticles, gapID)
	}

	ps := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), res.Plan, res.Index, res.Par2Files, fetch, ps, testLogger()); err != nil {
		t.Fatal(err)
	}
	got, ok := ps.Get(gapID)
	if !ok || !bytes.Equal(got, contents["a.rar"][2048:4096]) {
		t.Fatal("missing segment not rebuilt byte-exactly under its placeholder id")
	}
	if n := fetch.countFetches(gapID); n != 0 {
		t.Fatalf("placeholder fetched %d times; no provider can hold it", n)
	}
}

// The stored NZB of an imported release already carries the importer's gap
// placeholders. They are known-missing from the start: the plan must count
// them dead without asking a provider, and the repair must store the rebuilt
// payload under the placeholder id. Previously, with no other damage known,
// the plan found nothing to repair.
func TestResolveTreatsStoredGapPlaceholdersAsDead(t *testing.T) {
	fm, store, fetch, contents, deadID := mkResolveFixture(t, false)
	gapID := holes.PlaceholderID(2, "a.rar-0@test")
	for _, f := range store.Files {
		for _, s := range f.Segments {
			if s.Id == deadID {
				s.Id = gapID
			}
		}
	}

	res, err := Resolve(context.Background(), fm, store, nil, fetch,
		Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20}, testLogger(), nil)
	if err != nil {
		t.Fatalf("plan for a stored gap placeholder: %v", err)
	}
	if len(res.Plan.DeadArticles) != 1 || res.Plan.DeadArticles[0].MessageID != gapID {
		t.Fatalf("dead articles = %+v, want only the gap placeholder %s", res.Plan.DeadArticles, gapID)
	}

	ps := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), res.Plan, res.Index, res.Par2Files, fetch, ps, testLogger()); err != nil {
		t.Fatal(err)
	}
	if got, ok := ps.Get(gapID); !ok || !bytes.Equal(got, contents["a.rar"][2048:4096]) {
		t.Fatal("stored gap not rebuilt byte-exactly under its placeholder id")
	}
	if n := fetch.countFetches(gapID); n != 0 {
		t.Fatalf("placeholder fetched %d times; no provider can hold it", n)
	}
}
