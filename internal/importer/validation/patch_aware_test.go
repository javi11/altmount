package validation

import (
	"context"
	"testing"
	"time"

	"github.com/kipsilabs/altmount/internal/holes"
	metapb "github.com/kipsilabs/altmount/internal/metadata/proto"
	"github.com/kipsilabs/altmount/internal/testsupport/fakepool"
	"github.com/javi11/nntppool/v5"
)

// fakePatchIndex reports locally repaired articles.
type fakePatchIndex struct{ have map[string]bool }

func (f fakePatchIndex) Has(messageID string) bool { return f.have[messageID] }

// An article missing from every provider but present in the local patch store
// counts as available: repaired bytes never live on usenet, so without this a
// repaired release could never be (re-)imported.
func TestReleaseProbeTreatsPatchedSegmentAsAvailable(t *testing.T) {
	client := fakepool.New()
	segs := makeTestSegments("video", 20)
	// Kill one sampled segment on the providers.
	dead := segs[0].Id
	client.SetBehavior(dead, fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	files := []FastFailFile{{Filename: "movie.mkv", Segments: segs}}

	// Without a patch index: the probe reports damage.
	missing, err := FastFailReleaseProbe(context.Background(), files,
		fastFailPoolManager{client: client}, 100, 1, time.Second, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !missing {
		t.Fatal("missing = false, want true without a patch for the dead article")
	}

	// With the article in the patch store: the probe reports healthy.
	missing, err = FastFailReleaseProbe(context.Background(), files,
		fastFailPoolManager{client: client}, 100, 1, time.Second,
		fakePatchIndex{have: map[string]bool{dead: true}}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if missing {
		t.Fatal("missing = true, want false when the dead article has a local patch")
	}
}

// The per-file sweep must apply the same rule.
func TestCheckFilesTreatsPatchedSegmentAsAvailable(t *testing.T) {
	client := fakepool.New()
	segs := makeTestSegments("video", 10)
	dead := segs[0].Id
	client.SetBehavior(dead, fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	files := []FastFailFile{{Filename: "movie.mkv", Segments: segs}}

	results, err := FastFailCheckFiles(context.Background(), files,
		fastFailPoolManager{client: client}, 100, 1, time.Second, nil,
		fakePatchIndex{have: map[string]bool{dead: true}}, false, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Broken {
		t.Fatal("file marked broken despite the dead article having a local patch")
	}
	if len(results[0].MissingSegmentIDs) != 0 {
		t.Fatalf("MissingSegmentIDs = %v, want empty", results[0].MissingSegmentIDs)
	}
}

// A gap placeholder that PAR2 repair has rebuilt is served from the patch
// store, so it is no longer a known miss: a repaired import must not be
// parked for the same repair again. Unpatched placeholders still count.
func TestPlaceholderResultsSkipsPatchedGaps(t *testing.T) {
	p1, p2 := holes.PlaceholderID(2, "s"), holes.PlaceholderID(3, "s")
	files := []FastFailFile{
		{Filename: "release.part01.rar", GroupKey: "release", Segments: []*metapb.SegmentData{{Id: "a-1"}, {Id: p1}}},
		{Filename: "release.part02.rar", GroupKey: "release", Segments: []*metapb.SegmentData{{Id: "b-1"}, {Id: p2}}},
	}

	results := PlaceholderResults(files, fakePatchIndex{have: map[string]bool{p1: true}})

	if results[0].Broken || results[0].KnownGapCount != 0 || len(results[0].MissingSegmentIDs) != 0 {
		t.Errorf("fully patched file = %+v, want zero value", results[0])
	}
	got := results[1]
	if !got.Broken || got.KnownGapCount != 1 || len(got.MissingSegmentIDs) != 1 || got.MissingSegmentIDs[0] != p2 {
		t.Errorf("unpatched gap = %+v, want Broken with only %s", got, p2)
	}
}
