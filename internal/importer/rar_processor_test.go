package importer

import (
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/rarlist"
	"github.com/stretchr/testify/require"
)

// helper to build segment of size with implicit 0-based offsets
func seg(id string, size int64) *metapb.SegmentData {
	return &metapb.SegmentData{Id: id, StartOffset: 0, EndOffset: size - 1, SegmentSize: size}
}

func TestSlicePartSegmentsBasic(t *testing.T) {
	segments := []*metapb.SegmentData{seg("a", 10), seg("b", 10), seg("c", 5)} // total 25

	// slice starting at 5 length 10 (covers second half of a and first half of b)
	out, covered, err := slicePartSegments(segments, 5, 10)
	require.NoError(t, err)
	require.Equal(t, int64(10), covered)
	require.Len(t, out, 2)
	require.Equal(t, int64(5), out[0].StartOffset)
	require.Equal(t, int64(9), out[0].EndOffset)
	require.Equal(t, "a", out[0].Id)
	require.Equal(t, int64(0), out[1].StartOffset)
	require.Equal(t, int64(4), out[1].EndOffset)
	require.Equal(t, "b", out[1].Id)
}

func TestSlicePartSegmentsExactSegment(t *testing.T) {
	segments := []*metapb.SegmentData{seg("a", 10), seg("b", 10)}
	out, covered, err := slicePartSegments(segments, 10, 10)
	require.NoError(t, err)
	require.Equal(t, int64(10), covered)
	require.Len(t, out, 1)
	require.Equal(t, "b", out[0].Id)
	require.Equal(t, int64(0), out[0].StartOffset)
	require.Equal(t, int64(9), out[0].EndOffset)
}

func TestSlicePartSegmentsBeyondEnd(t *testing.T) {
	segments := []*metapb.SegmentData{seg("a", 5)}
	out, covered, err := slicePartSegments(segments, 3, 10) // only 2 bytes available
	require.NoError(t, err)
	require.Equal(t, int64(2), covered)
	require.Len(t, out, 1)
	require.Equal(t, int64(3), out[0].StartOffset)
	require.Equal(t, int64(4), out[0].EndOffset)
}

func TestConvertAggregatedFilesToRarContentSinglePart(t *testing.T) {
	rp := &rarProcessor{}
	rarFiles := []ParsedFile{{Filename: "vol1.rar", Segments: []*metapb.SegmentData{seg("s1", 100)}}}
	ag := []rarlist.AggregatedFile{{Name: "file.bin", TotalPackedSize: 60, Parts: []rarlist.AggregatedFilePart{{Path: "vol1.rar", DataOffset: 10, PackedSize: 60}}}}

	out, err := rp.convertAggregatedFilesToRarContent(ag, rarFiles)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Segments, 1)
	s := out[0].Segments[0]
	require.Equal(t, int64(10), s.StartOffset)
	require.Equal(t, int64(69), s.EndOffset)
}

func TestConvertAggregatedFilesToRarContentMultiPart(t *testing.T) {
	rp := &rarProcessor{}
	rarFiles := []ParsedFile{
		{Filename: "part1.rar", Segments: []*metapb.SegmentData{seg("p1s1", 50), seg("p1s2", 50)}}, // 100 bytes
		{Filename: "part2.rar", Segments: []*metapb.SegmentData{seg("p2s1", 30), seg("p2s2", 30)}}, // 60 bytes
	}
	ag := []rarlist.AggregatedFile{{
		Name:            "movie.mkv",
		TotalPackedSize: 120,
		Parts: []rarlist.AggregatedFilePart{
			{Path: "part1.rar", DataOffset: 20, PackedSize: 80}, // last 30 of first seg + all second seg (50) => 30+50=80
			{Path: "part2.rar", DataOffset: 0, PackedSize: 40},  // all first seg (30) + 10 of second seg
		},
	}}

	out, err := rp.convertAggregatedFilesToRarContent(ag, rarFiles)
	require.NoError(t, err)
	require.Len(t, out, 1)
	got := out[0]
	// Expect 4 segments: tail of p1s1, full p1s2, full p2s1, head of p2s2
	require.Len(t, got.Segments, 4)
	// tail of p1s1
	require.Equal(t, "p1s1", got.Segments[0].Id)
	require.Equal(t, int64(20), got.Segments[0].StartOffset)
	require.Equal(t, int64(49), got.Segments[0].EndOffset)
	// full p1s2
	require.Equal(t, "p1s2", got.Segments[1].Id)
	require.Equal(t, int64(0), got.Segments[1].StartOffset)
	require.Equal(t, int64(49), got.Segments[1].EndOffset)
	// full p2s1
	require.Equal(t, "p2s1", got.Segments[2].Id)
	require.Equal(t, int64(0), got.Segments[2].StartOffset)
	require.Equal(t, int64(29), got.Segments[2].EndOffset)
	// head p2s2
	require.Equal(t, "p2s2", got.Segments[3].Id)
	require.Equal(t, int64(0), got.Segments[3].StartOffset)
	require.Equal(t, int64(9), got.Segments[3].EndOffset)
}
