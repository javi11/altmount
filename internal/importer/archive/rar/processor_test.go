package rar

import (
	"context"
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/rardecode/v2"
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
	rarFiles := []parser.ParsedFile{{Filename: "vol1.rar", Segments: []*metapb.SegmentData{seg("s1", 100)}}}
	ag := []rardecode.ArchiveFileInfo{{Name: "file.bin", TotalPackedSize: 60, Parts: []rardecode.FilePartInfo{{Path: "vol1.rar", DataOffset: 10, PackedSize: 60}}}}

	out, err := rp.convertAggregatedFilesToRarContent(context.Background(), ag, rarFiles)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Segments, 1)
	s := out[0].Segments[0]
	require.Equal(t, int64(10), s.StartOffset)
	require.Equal(t, int64(69), s.EndOffset)
}

func TestConvertAggregatedFilesToRarContentMultiPart(t *testing.T) {
	rp := &rarProcessor{}
	rarFiles := []parser.ParsedFile{
		{Filename: "part1.rar", Segments: []*metapb.SegmentData{seg("p1s1", 50), seg("p1s2", 50)}}, // 100 bytes
		{Filename: "part2.rar", Segments: []*metapb.SegmentData{seg("p2s1", 30), seg("p2s2", 30)}}, // 60 bytes
	}
	ag := []rardecode.ArchiveFileInfo{{
		Name:            "movie.mkv",
		TotalPackedSize: 120,
		Parts: []rardecode.FilePartInfo{
			{Path: "part1.rar", DataOffset: 20, PackedSize: 80}, // last 30 of first seg + all second seg (50) => 30+50=80
			{Path: "part2.rar", DataOffset: 0, PackedSize: 40},  // all first seg (30) + 10 of second seg
		},
	}}

	out, err := rp.convertAggregatedFilesToRarContent(context.Background(), ag, rarFiles)
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

func TestPatchMissingSegment_NoPatchingNeeded(t *testing.T) {
	segments := []*metapb.SegmentData{seg("s1", 768000), seg("s2", 768000)}
	expectedSize := int64(1536000)
	coveredSize := int64(1536000)

	patched, newCovered, err := patchMissingSegment(segments, expectedSize, coveredSize)
	require.NoError(t, err)
	require.Equal(t, segments, patched, "segments should not be modified")
	require.Equal(t, coveredSize, newCovered, "covered size should remain the same")
	require.Len(t, patched, 2, "should have original 2 segments")
}

func TestPatchMissingSegment_SmallShortfall(t *testing.T) {
	segments := []*metapb.SegmentData{seg("s1", 768000)}
	expectedSize := int64(800000)
	coveredSize := int64(768000)
	shortfall := int64(32000)

	patched, newCovered, err := patchMissingSegment(segments, expectedSize, coveredSize)
	require.NoError(t, err)
	require.Len(t, patched, 2, "should have original segment + patch segment")
	require.Equal(t, expectedSize, newCovered, "should cover full expected size")

	// Verify the patch segment
	patchSeg := patched[1]
	require.Equal(t, "s1", patchSeg.Id, "patch should duplicate last segment ID")
	require.Equal(t, int64(0), patchSeg.StartOffset, "patch should start at offset 0")
	require.Equal(t, shortfall-1, patchSeg.EndOffset, "patch should cover shortfall bytes")
	require.Equal(t, int64(768000), patchSeg.SegmentSize, "patch should have same segment size")
}

func TestPatchMissingSegment_ExactThreshold(t *testing.T) {
	segments := []*metapb.SegmentData{seg("s1", 500000)}
	expectedSize := int64(1300000) // 800000 bytes shortfall
	coveredSize := int64(500000)

	patched, newCovered, err := patchMissingSegment(segments, expectedSize, coveredSize)
	require.NoError(t, err, "should succeed at exact threshold")
	require.Len(t, patched, 2, "should have original segment + patch segment")
	require.Equal(t, expectedSize, newCovered, "should cover full expected size")

	// Verify the patch segment
	patchSeg := patched[1]
	require.Equal(t, int64(799999), patchSeg.EndOffset, "patch should cover exactly 800000 bytes")
}

func TestPatchMissingSegment_ExceedsThreshold(t *testing.T) {
	segments := []*metapb.SegmentData{seg("s1", 500000)}
	expectedSize := int64(1300001) // 800001 bytes shortfall - exceeds threshold
	coveredSize := int64(500000)

	patched, newCovered, err := patchMissingSegment(segments, expectedSize, coveredSize)
	require.Error(t, err, "should fail when shortfall exceeds threshold")
	require.Nil(t, patched, "should return nil on error")
	require.Equal(t, int64(0), newCovered, "should return 0 covered on error")
	require.Contains(t, err.Error(), "exceeds single segment threshold", "error should mention threshold")
}

func TestPatchMissingSegment_NoSegmentsAvailable(t *testing.T) {
	segments := []*metapb.SegmentData{}
	expectedSize := int64(50000)
	coveredSize := int64(0)

	patched, newCovered, err := patchMissingSegment(segments, expectedSize, coveredSize)
	require.Error(t, err, "should fail when no segments available to duplicate")
	require.Nil(t, patched, "should return nil on error")
	require.Equal(t, int64(0), newCovered, "should return 0 covered on error")
	require.Contains(t, err.Error(), "no segments available", "error should mention no segments")
}

func TestPatchMissingSegment_TypicalSegmentGap(t *testing.T) {
	// Simulate typical scenario: missing one ~768KB segment at end of part
	segments := []*metapb.SegmentData{
		seg("s1", 768000),
		seg("s2", 768000),
		seg("s3", 768000),
	}
	expectedSize := int64(3072000)
	coveredSize := int64(2304000) // missing ~768KB

	patched, newCovered, err := patchMissingSegment(segments, expectedSize, coveredSize)
	require.NoError(t, err, "should successfully patch typical segment gap")
	require.Len(t, patched, 4, "should have 3 original + 1 patch segment")
	require.Equal(t, expectedSize, newCovered, "should cover full expected size")

	// Verify the patch segment duplicates the last segment
	patchSeg := patched[3]
	require.Equal(t, "s3", patchSeg.Id, "should duplicate last segment")
	require.Equal(t, int64(767999), patchSeg.EndOffset, "should cover exactly 768000 bytes")
}

func TestPatchMissingSegment_MultipleSegmentsMissing(t *testing.T) {
	segments := []*metapb.SegmentData{seg("s1", 768000)}
	expectedSize := int64(3000000) // missing ~2.2MB
	coveredSize := int64(768000)

	patched, newCovered, err := patchMissingSegment(segments, expectedSize, coveredSize)
	require.Error(t, err, "should fail when multiple segments missing")
	require.Nil(t, patched, "should return nil on error")
	require.Equal(t, int64(0), newCovered, "should return 0 covered on error")
}

func TestPatchMissingSegment_VerySmallShortfall(t *testing.T) {
	segments := []*metapb.SegmentData{seg("s1", 768000)}
	expectedSize := int64(768100)
	coveredSize := int64(768000)
	shortfall := int64(100)

	patched, newCovered, err := patchMissingSegment(segments, expectedSize, coveredSize)
	require.NoError(t, err, "should handle very small shortfalls")
	require.Len(t, patched, 2, "should have original segment + patch segment")
	require.Equal(t, expectedSize, newCovered, "should cover full expected size")

	// Verify the patch segment
	patchSeg := patched[1]
	require.Equal(t, shortfall-1, patchSeg.EndOffset, "patch should cover exactly 100 bytes")
}
