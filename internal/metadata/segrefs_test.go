package metadata

import (
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSegDataToRefs_Basic(t *testing.T) {
	index := map[string]int64{"msg-001": 0, "msg-002": 1, "msg-003": 2}
	segments := []*metapb.SegmentData{
		{Id: "msg-001", StartOffset: 0, EndOffset: 9999, SegmentSize: 10000},
		{Id: "msg-002", StartOffset: 0, EndOffset: 9999, SegmentSize: 10000},
		{Id: "msg-003", StartOffset: 1000, EndOffset: 8000, SegmentSize: 10000},
	}

	refs, err := segDataToRefs(segments, index)
	require.NoError(t, err)
	require.Len(t, refs, 3)
	assert.Equal(t, int64(0), refs[0].StoreIndex)
	assert.Equal(t, int64(0), refs[0].StartOffset)
	assert.Equal(t, int64(9999), refs[0].EndOffset)
	assert.Equal(t, int64(1), refs[1].StoreIndex)
	assert.Equal(t, int64(2), refs[2].StoreIndex)
	assert.Equal(t, int64(1000), refs[2].StartOffset)
	assert.Equal(t, int64(8000), refs[2].EndOffset)
}

func TestSegDataToRefs_Nil(t *testing.T) {
	refs, err := segDataToRefs(nil, nil)
	require.NoError(t, err)
	assert.Nil(t, refs)
}

func TestSegDataToRefs_Empty(t *testing.T) {
	refs, err := segDataToRefs([]*metapb.SegmentData{}, map[string]int64{})
	require.NoError(t, err)
	assert.Nil(t, refs)
}

func TestSegDataToRefs_PreservesSlicedOffsets(t *testing.T) {
	// Archive processors call slicePartSegments which narrows StartOffset/EndOffset.
	// segDataToRefs must preserve the narrowed offsets, not reset them.
	index := map[string]int64{"seg-big": 5}
	segments := []*metapb.SegmentData{
		{Id: "seg-big", StartOffset: 4096, EndOffset: 8191, SegmentSize: 10000},
	}

	refs, err := segDataToRefs(segments, index)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, int64(5), refs[0].StoreIndex)
	assert.Equal(t, int64(4096), refs[0].StartOffset, "must preserve narrowed StartOffset from archive slicing")
	assert.Equal(t, int64(8191), refs[0].EndOffset, "must preserve narrowed EndOffset from archive slicing")
}

func TestSegDataToRefs_MissingIndexKey(t *testing.T) {
	index := map[string]int64{"msg-001": 0}
	segments := []*metapb.SegmentData{
		{Id: "msg-UNKNOWN", StartOffset: 0, EndOffset: 9999, SegmentSize: 10000},
	}

	_, err := segDataToRefs(segments, index)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "msg-UNKNOWN")
}

func TestSplitRefs_UniformBodyWithSmallerLast(t *testing.T) {
	// Uniform body of 3 same-size segments collapses to one run; the smaller
	// trailing segment is a lone full-use ref kept explicit.
	refs := []*metapb.SegmentRef{
		{StoreIndex: 10, StartOffset: 0, EndOffset: 9999, DecodedBytes: 10000},
		{StoreIndex: 11, StartOffset: 0, EndOffset: 9999, DecodedBytes: 10000},
		{StoreIndex: 12, StartOffset: 0, EndOffset: 9999, DecodedBytes: 10000},
		{StoreIndex: 13, StartOffset: 0, EndOffset: 4095, DecodedBytes: 4096},
	}
	runs, leftover := splitRefs(refs)
	require.Len(t, runs, 1)
	assert.Equal(t, int64(10), runs[0].BaseStoreIndex)
	assert.Equal(t, int64(3), runs[0].Count)
	assert.Equal(t, int64(10000), runs[0].DecodedBytes)
	require.Len(t, leftover, 1)
	assert.Equal(t, int64(13), leftover[0].StoreIndex)
}

func TestSplitRefs_PartialSeams(t *testing.T) {
	// The RAR shape: a partial leading segment (header offset), a uniform body,
	// and a partial trailing segment. Body folds into a run; the two partial
	// seam segments stay explicit.
	refs := []*metapb.SegmentRef{
		{StoreIndex: 1, StartOffset: 366, EndOffset: 716799, DecodedBytes: 716800}, // partial head
		{StoreIndex: 2, StartOffset: 0, EndOffset: 716799, DecodedBytes: 716800},
		{StoreIndex: 3, StartOffset: 0, EndOffset: 716799, DecodedBytes: 716800},
		{StoreIndex: 4, StartOffset: 0, EndOffset: 716799, DecodedBytes: 716800},
		{StoreIndex: 5, StartOffset: 0, EndOffset: 38285, DecodedBytes: 716800}, // partial tail
	}
	runs, leftover := splitRefs(refs)
	require.Len(t, runs, 1)
	assert.Equal(t, int64(2), runs[0].BaseStoreIndex)
	assert.Equal(t, int64(3), runs[0].Count)
	require.Len(t, leftover, 2)
	assert.Equal(t, int64(1), leftover[0].StoreIndex)
	assert.Equal(t, int64(5), leftover[1].StoreIndex)
}

func TestSplitRefs_NotStrictlyIncreasing(t *testing.T) {
	// When store indices are not strictly increasing, the merge-by-index read
	// would lose order, so splitRefs keeps everything explicit and emits no runs.
	refs := []*metapb.SegmentRef{
		{StoreIndex: 5, StartOffset: 0, EndOffset: 9999, DecodedBytes: 10000},
		{StoreIndex: 1, StartOffset: 0, EndOffset: 9999, DecodedBytes: 10000},
		{StoreIndex: 2, StartOffset: 0, EndOffset: 9999, DecodedBytes: 10000},
	}
	runs, leftover := splitRefs(refs)
	assert.Nil(t, runs)
	assert.Equal(t, refs, leftover)
}

func TestSplitRefs_GapBreaksRun(t *testing.T) {
	// A gap in store indices splits what would be one run; non-adjacent full-use
	// singletons stay explicit.
	refs := []*metapb.SegmentRef{
		{StoreIndex: 0, StartOffset: 0, EndOffset: 9999, DecodedBytes: 10000},
		{StoreIndex: 2, StartOffset: 0, EndOffset: 9999, DecodedBytes: 10000},
	}
	runs, leftover := splitRefs(refs)
	assert.Empty(t, runs)
	assert.Len(t, leftover, 2)
}

func TestSplitRefs_Empty(t *testing.T) {
	runs, leftover := splitRefs(nil)
	assert.Nil(t, runs)
	assert.Nil(t, leftover)
}
