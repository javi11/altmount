package par2repair

import (
	"context"
	"testing"

	"github.com/javi11/nzbparser"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// nzbFromFixture rebuilds the same release as an *nzbparser.Nzb, so the two
// resolvers can be compared on identical input.
func nzbFromFixture(fm *metapb.FileMetadata, store *metapb.NzbStore) *nzbparser.Nzb {
	par2ByID := map[string]string{}
	for _, p := range fm.Par2Files {
		for _, seg := range p.SegmentData {
			par2ByID[normalizeMsgID(seg.Id)] = p.Filename
		}
	}

	n := &nzbparser.Nzb{}
	for _, f := range store.Files {
		file := nzbparser.NzbFile{Subject: f.Subject}
		// Name the file the way the importer would: PAR2 entries by their
		// recorded filename, content entries from the subject's quoted name.
		if len(f.Segments) > 0 {
			if name, ok := par2ByID[normalizeMsgID(f.Segments[0].Id)]; ok {
				file.Filename = name
			}
		}
		if file.Filename == "" {
			file.Filename = subjectFilename(f.Subject)
		}
		for _, s := range f.Segments {
			file.Segments = append(file.Segments, nzbparser.NzbSegment{
				ID:     s.Id,
				Number: int(s.Number),
				Bytes:  int(s.Bytes),
			})
		}
		file.TotalSegments = len(file.Segments)
		n.Files = append(n.Files, file)
	}
	n.TotalFiles = len(n.Files)
	return n
}

// The NZB-mode resolver must plan the same repair as the metadata-mode
// resolver for the same release — same missing slices, same dead articles.
func TestResolveFromNzbMatchesMetadataResolver(t *testing.T) {
	fm, store, fetch, _, deadID := mkResolveFixture(t, false)
	caps := Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20}

	fromMeta, err := Resolve(context.Background(), fm, store, []string{deadID}, fetch, caps, testLogger(), nil)
	if err != nil {
		t.Fatalf("metadata resolve: %v", err)
	}

	n := nzbFromFixture(fm, store)
	fromNzb, err := ResolveFromNzb(context.Background(), n, []string{deadID}, fetch, caps, testLogger(), nil)
	if err != nil {
		t.Fatalf("nzb resolve: %v", err)
	}

	if !equalInts(fromNzb.Plan.Missing, fromMeta.Plan.Missing) {
		t.Fatalf("Missing = %v, want %v", fromNzb.Plan.Missing, fromMeta.Plan.Missing)
	}
	if fromNzb.Plan.SliceSize != fromMeta.Plan.SliceSize {
		t.Fatalf("SliceSize = %d, want %d", fromNzb.Plan.SliceSize, fromMeta.Plan.SliceSize)
	}
	if fromNzb.Plan.GlobalSlices != fromMeta.Plan.GlobalSlices {
		t.Fatalf("GlobalSlices = %d, want %d", fromNzb.Plan.GlobalSlices, fromMeta.Plan.GlobalSlices)
	}
	if len(fromNzb.Plan.DeadArticles) != 1 || fromNzb.Plan.DeadArticles[0].MessageID != normalizeMsgID(deadID) {
		t.Fatalf("DeadArticles = %+v", fromNzb.Plan.DeadArticles)
	}
}

// An NZB-mode repair produces the same byte-exact patch as metadata mode.
func TestResolveFromNzbRepairsEndToEnd(t *testing.T) {
	fm, store, fetch, contents, deadID := mkResolveFixture(t, false)
	n := nzbFromFixture(fm, store)

	res, err := ResolveFromNzb(context.Background(), n, []string{deadID}, fetch,
		Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20}, testLogger(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ps := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), res.Plan, res.Index, res.Par2Files, fetch, ps, testLogger()); err != nil {
		t.Fatal(err)
	}
	got, ok := ps.Get(normalizeMsgID(deadID))
	if !ok || string(got) != string(contents["a.rar"][2048:4096]) {
		t.Fatal("NZB-mode repair did not reproduce the dead article byte-exactly")
	}
}

func TestResolveFromNzbWithoutPar2Files(t *testing.T) {
	fm, store, fetch, _, _ := mkResolveFixture(t, false)
	n := nzbFromFixture(fm, store)
	// Drop every PAR2 file from the NZB.
	var kept []nzbparser.NzbFile
	for _, f := range n.Files {
		if !isPar2Filename(f.Filename) {
			kept = append(kept, f)
		}
	}
	n.Files = kept

	_, err := ResolveFromNzb(context.Background(), n, nil, fetch, Caps{}, testLogger(), nil)
	if err == nil {
		t.Fatal("want error when the NZB carries no PAR2 files")
	}
}
