package metadata

import (
	"fmt"
	"path/filepath"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func sampleStore() *metapb.NzbStore {
	return &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Subject: "Movie.mkv yEnc (1/2)", Poster: "p@x", Date: 1000, Groups: []string{"a.b.test"},
			Segments: []*metapb.NzbSeg{{Id: "m1@x", Number: 1, Bytes: 700000}, {Id: "m2@x", Number: 2, Bytes: 500000}}},
		{Subject: "Movie.par2 yEnc (1/1)", Poster: "p@x", Date: 1000, Groups: []string{"a.b.test"},
			Segments: []*metapb.NzbSeg{{Id: "p1@x", Number: 1, Bytes: 4096}}},
	}}
}

func TestStoreService_WriteRead(t *testing.T) {
	ss := NewStoreService(t.TempDir())
	ref := filepath.Join(t.TempDir(), "rel.nzbz")
	orig := sampleStore()
	require.NoError(t, ss.WriteStore(ref, orig))

	got, err := ss.ReadStore(ref)
	require.NoError(t, err)
	require.True(t, proto.Equal(orig, got))

	// flat index: file0 seg0, file0 seg1, file1 seg0
	flat := FlatSegments(got)
	require.Len(t, flat, 3)
	assert.Equal(t, "m1@x", flat[0].Id)
	assert.Equal(t, "m2@x", flat[1].Id)
	assert.Equal(t, "p1@x", flat[2].Id)
}

func TestResolveRefs(t *testing.T) {
	store := sampleStore()
	flat := FlatSegments(store)
	refs := []*metapb.SegmentRef{
		{StoreIndex: 0, StartOffset: 0, EndOffset: 699999},
		{StoreIndex: 2, StartOffset: 10, EndOffset: 4095},
	}
	segs, err := resolveRefs(flat, refs)
	require.NoError(t, err)
	require.Len(t, segs, 2)
	assert.Equal(t, "m1@x", segs[0].Id)
	assert.Equal(t, int64(700000), segs[0].SegmentSize)
	assert.Equal(t, int64(0), segs[0].StartOffset)
	assert.Equal(t, "p1@x", segs[1].Id)
	assert.Equal(t, int64(4096), segs[1].SegmentSize)
	assert.Equal(t, int64(10), segs[1].StartOffset)

	_, err = resolveRefs(flat, []*metapb.SegmentRef{{StoreIndex: 99}})
	assert.Error(t, err, "out-of-range index must error")
}

func TestResolveRuns(t *testing.T) {
	store := sampleStore()
	flat := FlatSegments(store)

	// One run covering the two segments of file0, with a smaller trailing segment
	// expressed as a second run (uniform-yEnc shape).
	runs := []*metapb.SegmentRun{
		{BaseStoreIndex: 0, Count: 1, DecodedBytes: 700000},
		{BaseStoreIndex: 1, Count: 1, DecodedBytes: 500000},
	}
	segs, err := resolveRuns(flat, runs)
	require.NoError(t, err)
	require.Len(t, segs, 2)
	assert.Equal(t, "m1@x", segs[0].Id)
	assert.Equal(t, int64(700000), segs[0].SegmentSize)
	assert.Equal(t, int64(0), segs[0].StartOffset)
	assert.Equal(t, int64(699999), segs[0].EndOffset)
	assert.Equal(t, "m2@x", segs[1].Id)
	assert.Equal(t, int64(500000), segs[1].SegmentSize)
	assert.Equal(t, int64(499999), segs[1].EndOffset)

	// DecodedBytes=0 falls back to the store's NzbSeg.bytes.
	segs, err = resolveRuns(flat, []*metapb.SegmentRun{{BaseStoreIndex: 2, Count: 1}})
	require.NoError(t, err)
	require.Len(t, segs, 1)
	assert.Equal(t, "p1@x", segs[0].Id)
	assert.Equal(t, int64(4096), segs[0].SegmentSize)
	assert.Equal(t, int64(4095), segs[0].EndOffset)

	// Out-of-range run index must error.
	_, err = resolveRuns(flat, []*metapb.SegmentRun{{BaseStoreIndex: 2, Count: 5}})
	assert.Error(t, err, "out-of-range run must error")

	// Empty input returns nil, nil.
	got, err := resolveRuns(flat, nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestResolveSegments_MixedMatchesExplicit is the correctness anchor for the
// mixed run+ref encoding: splitting a ref slice into runs + leftover refs and
// resolving via resolveSegments must reproduce byte-identical SegmentData (same
// ids, sizes, offsets, AND order) as resolving the original explicit refs.
func TestResolveSegments_MixedMatchesExplicit(t *testing.T) {
	// 8-segment flat store; sizes don't matter for the merge, ids do (order check).
	flat := make([]*metapb.NzbSeg, 8)
	for i := range flat {
		flat[i] = &metapb.NzbSeg{Id: fmt.Sprintf("seg-%d", i), Number: int32(i + 1), Bytes: 1000}
	}

	// RAR-like shape: partial head (idx0), uniform body (idx1..5), partial tail (idx6),
	// then another full singleton (idx7).
	refs := []*metapb.SegmentRef{
		{StoreIndex: 0, StartOffset: 100, EndOffset: 999, DecodedBytes: 1000}, // partial head
		{StoreIndex: 1, StartOffset: 0, EndOffset: 999, DecodedBytes: 1000},
		{StoreIndex: 2, StartOffset: 0, EndOffset: 999, DecodedBytes: 1000},
		{StoreIndex: 3, StartOffset: 0, EndOffset: 999, DecodedBytes: 1000},
		{StoreIndex: 4, StartOffset: 0, EndOffset: 999, DecodedBytes: 1000},
		{StoreIndex: 5, StartOffset: 0, EndOffset: 999, DecodedBytes: 1000},
		{StoreIndex: 6, StartOffset: 0, EndOffset: 499, DecodedBytes: 1000}, // partial tail
		{StoreIndex: 7, StartOffset: 0, EndOffset: 999, DecodedBytes: 1000},
	}

	// Baseline: all explicit.
	want, err := resolveRefs(flat, refs)
	require.NoError(t, err)

	// Mixed: split then resolve via the merge path.
	runs, leftover := splitRefs(refs)
	require.NotEmpty(t, runs, "uniform body must fold into a run")
	require.NotEmpty(t, leftover, "partial seams must stay explicit")

	got, err := resolveSegments(flat, runs, leftover)
	require.NoError(t, err)
	require.Len(t, got, len(want))
	for i := range want {
		assert.Truef(t, proto.Equal(want[i], got[i]),
			"segment %d mismatch: want %+v got %+v", i, want[i], got[i])
	}
}
