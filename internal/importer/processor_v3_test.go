package importer

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
