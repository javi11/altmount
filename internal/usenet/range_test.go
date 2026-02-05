package usenet

import (
	"context"
	"testing"
)

type mockLoader struct { // implements SegmentLoader
	segments []Segment
	groups   [][]string
}

func (m *mockLoader) GetSegment(i int) (Segment, []string, bool) {
	if i < 0 || i >= len(m.segments) {
		return Segment{}, nil, false
	}
	return m.segments[i], m.groups[i], true
}

// helper to collect lengths
func collectedLen(r *segmentRange) int64 {
	var total int64
	for _, s := range r.segments {
		if s != nil {
			total += (s.End - s.Start + 1)
		}
	}
	return total
}

func TestGetSegmentsInRange_BasicFullCoverage(t *testing.T) {
	// Two segments, no internal start offset
	loader := &mockLoader{segments: []Segment{
		{Id: "s1", Start: 0, End: 9, Size: 10}, // contributes bytes 0..9
		{Id: "s2", Start: 0, End: 9, Size: 10}, // contributes bytes 10..19
	}, groups: [][]string{{}, {}}}

	rg := GetSegmentsInRange(context.Background(), 0, 19, loader)
	if len(rg.segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(rg.segments))
	}
	// First segment should not be trimmed
	if rg.segments[0].Start != 0 || rg.segments[0].End != 9 {
		t.Fatalf("unexpected first segment bounds: %d-%d", rg.segments[0].Start, rg.segments[0].End)
	}
	if rg.segments[1].Start != 0 || rg.segments[1].End != 9 {
		t.Fatalf("unexpected second segment bounds: %d-%d", rg.segments[1].Start, rg.segments[1].End)
	}
	if collectedLen(rg) != 20 {
		t.Fatalf("collected length mismatch: got %d want 20", collectedLen(rg))
	}
}

func TestGetSegmentsInRange_PartialFirstAndLast(t *testing.T) {
	loader := &mockLoader{segments: []Segment{
		{Id: "s1", Start: 0, End: 9, Size: 10}, // file 0..9
		{Id: "s2", Start: 0, End: 9, Size: 10}, // file 10..19
		{Id: "s3", Start: 0, End: 9, Size: 10}, // file 20..29
	}, groups: [][]string{{}, {}, {}}}

	// request middle bytes 5..24 (length 20)
	rg := GetSegmentsInRange(context.Background(), 5, 24, loader)
	if len(rg.segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(rg.segments))
	}
	// First trimmed to 5..9
	if rg.segments[0].Start != 5 || rg.segments[0].End != 9 {
		t.Fatalf("unexpected first segment trimmed bounds: %d-%d", rg.segments[0].Start, rg.segments[0].End)
	}
	// Middle full
	if rg.segments[1].Start != 0 || rg.segments[1].End != 9 {
		t.Fatalf("unexpected middle segment bounds: %d-%d", rg.segments[1].Start, rg.segments[1].End)
	}
	// Last trimmed 0..4
	if rg.segments[2].Start != 0 || rg.segments[2].End != 4 {
		t.Fatalf("unexpected last segment trimmed bounds: %d-%d", rg.segments[2].Start, rg.segments[2].End)
	}
	if collectedLen(rg) != 20 {
		t.Fatalf("collected length mismatch: got %d want 20", collectedLen(rg))
	}
}

func TestGetSegmentsInRange_InternalStartOffset(t *testing.T) {
	// Each segment has internal Start offset meaning usable data smaller than physical size
	loader := &mockLoader{segments: []Segment{
		{Id: "s1", Start: 2, End: 9, Size: 10}, // usable length 8 -> file 0..7 maps to physical 2..9
		{Id: "s2", Start: 1, End: 8, Size: 9},  // usable length 8 -> file 8..15 maps to physical 1..8
	}, groups: [][]string{{}, {}}}

	// Request spans partially across both segments
	rg := GetSegmentsInRange(context.Background(), 3, 12, loader) // length 10
	if len(rg.segments) != 2 {
		t.Fatalf("expected 2 segments got %d", len(rg.segments))
	}
	// First segment: request starts at file offset 3 -> 3 within segment usable => physical 2+3=5
	if rg.segments[0].Start != 5 || rg.segments[0].End != 9 { // trimmed tail because file portion covers up to logical 7
		// Actually request end 12 => first segment contributes logical 3..7 -> physical 5..9
		// so End should be 9
		// Start validated above
		// Use above conditional for failure
		if rg.segments[0].Start != 5 || rg.segments[0].End != 9 {
			v0 := rg.segments[0]
			t.Fatalf("unexpected first segment bounds: %d-%d", v0.Start, v0.End)
		}
	}
	// Second segment should start at its internal 1 + (requested logical 8 - segment logical base 8)=1, may trim end to cover up to logical 12
	// logical coverage second segment: base 8..15, need 8..12 => first 5 bytes => physical 1..5
	if rg.segments[1].Start != 1 || rg.segments[1].End != 5 {
		v1 := rg.segments[1]
		t.Fatalf("unexpected second segment bounds: %d-%d", v1.Start, v1.End)
	}
	if collectedLen(rg) != 10 {
		t.Fatalf("collected length mismatch got %d want 10", collectedLen(rg))
	}
}

func TestGetSegmentsInRange_RangeOutside(t *testing.T) {
	loader := &mockLoader{segments: []Segment{{Id: "s1", Start: 0, End: 4, Size: 5}}, groups: [][]string{{}}}
	// Request beyond available data (file length = 5)
	rg := GetSegmentsInRange(context.Background(), 10, 20, loader)
	if len(rg.segments) != 0 {
		t.Fatalf("expected 0 segments, got %d", len(rg.segments))
	}
}

func TestGetSegmentsInRange_EmptySegmentsOrZeroUsable(t *testing.T) {
	loader := &mockLoader{segments: []Segment{
		{Id: "s1", Start: 5, End: 4, Size: 5}, // usable 0 (End < Start)
		{Id: "s2", Start: 0, End: 3, Size: 4}, // usable 4 -> file 0..3
	}, groups: [][]string{{}, {}}}
	rg := GetSegmentsInRange(context.Background(), 1, 2, loader)
	if len(rg.segments) != 1 {
		t.Fatalf("expected 1 usable segment, got %d", len(rg.segments))
	}
	if rg.segments[0].Start != 1 || rg.segments[0].End != 2 {
		v := rg.segments[0]
		t.Fatalf("unexpected bounds %d-%d", v.Start, v.End)
	}
}

func TestGetSegmentsInRange_SingleSegmentTrimmed(t *testing.T) {
	loader := &mockLoader{segments: []Segment{{Id: "s1", Start: 0, End: 99, Size: 100}}, groups: [][]string{{}}}
	rg := GetSegmentsInRange(context.Background(), 10, 49, loader)
	if len(rg.segments) != 1 {
		t.Fatalf("expected 1 segment got %d", len(rg.segments))
	}
	if rg.segments[0].Start != 10 || rg.segments[0].End != 49 {
		t.Fatalf("unexpected bounds %d-%d", rg.segments[0].Start, rg.segments[0].End)
	}
	if collectedLen(rg) != 40 {
		t.Fatalf("length mismatch got %d want 40", collectedLen(rg))
	}
}

func TestGetSegmentsInRange_SingleSegmentInternalOffset(t *testing.T) {
	// Physical size 50, internal usable starts at 5 => usable length 45 -> logical file 0..44
	loader := &mockLoader{segments: []Segment{{Id: "s1", Start: 5, End: 49, Size: 50}}, groups: [][]string{{}}}
	rg := GetSegmentsInRange(context.Background(), 0, 9, loader) // first 10 logical bytes
	if len(rg.segments) != 1 {
		t.Fatalf("expected 1 segment got %d", len(rg.segments))
	}
	// Should map to physical 5..14
	if rg.segments[0].Start != 5 || rg.segments[0].End != 14 {
		t.Fatalf("unexpected bounds %d-%d", rg.segments[0].Start, rg.segments[0].End)
	}
	if collectedLen(rg) != 10 {
		t.Fatalf("length mismatch got %d want 10", collectedLen(rg))
	}
}

func TestGetSegmentsInRange_SingleByteMiddleSegment(t *testing.T) {
	loader := &mockLoader{segments: []Segment{
		{Id: "s1", Start: 0, End: 9, Size: 10}, // logical 0..9
		{Id: "s2", Start: 0, End: 9, Size: 10}, // logical 10..19
		{Id: "s3", Start: 0, End: 9, Size: 10}, // logical 20..29
	}, groups: [][]string{{}, {}, {}}}
	rg := GetSegmentsInRange(context.Background(), 10, 10, loader)
	if len(rg.segments) != 1 {
		t.Fatalf("expected 1 segment got %d", len(rg.segments))
	}
	if rg.segments[0].Id != "s2" {
		t.Fatalf("expected s2 got %s", rg.segments[0].Id)
	}
	if rg.segments[0].Start != 0 || rg.segments[0].End != 0 {
		t.Fatalf("unexpected bounds %d-%d", rg.segments[0].Start, rg.segments[0].End)
	}
	if collectedLen(rg) != 1 {
		t.Fatalf("length mismatch got %d want 1", collectedLen(rg))
	}
}

func TestGetSegmentsInRangeFromIndex_SkipToMiddle(t *testing.T) {
	// 10 segments of 10 bytes each (100 bytes total)
	segments := make([]Segment, 10)
	groups := make([][]string, 10)
	for i := range segments {
		segments[i] = Segment{Id: string(rune('a' + i)), Start: 0, End: 9, Size: 10}
		groups[i] = []string{}
	}
	loader := &mockLoader{segments: segments, groups: groups}

	// Request bytes 55-64 using index hint to start at segment 5 (offset 50)
	// This tests O(log n) skip - we provide startSegmentIndex=5, startFilePos=50
	rg := GetSegmentsInRangeFromIndex(context.Background(), 55, 64, loader, 5, 50)
	if len(rg.segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(rg.segments))
	}
	// First segment (index 5) should be trimmed to start at offset 5 (55-50=5)
	if rg.segments[0].Id != "f" || rg.segments[0].Start != 5 || rg.segments[0].End != 9 {
		t.Fatalf("unexpected first segment: id=%s start=%d end=%d", rg.segments[0].Id, rg.segments[0].Start, rg.segments[0].End)
	}
	// Second segment (index 6) should be trimmed to end at offset 4 (64-60=4)
	if rg.segments[1].Id != "g" || rg.segments[1].Start != 0 || rg.segments[1].End != 4 {
		t.Fatalf("unexpected second segment: id=%s start=%d end=%d", rg.segments[1].Id, rg.segments[1].Start, rg.segments[1].End)
	}
	if collectedLen(rg) != 10 {
		t.Fatalf("length mismatch got %d want 10", collectedLen(rg))
	}
}

func TestGetSegmentsInRangeFromIndex_EquivalentToBasic(t *testing.T) {
	// Verify that GetSegmentsInRangeFromIndex with startIndex=0, startPos=0
	// produces the same result as GetSegmentsInRange
	loader := &mockLoader{segments: []Segment{
		{Id: "s1", Start: 0, End: 9, Size: 10}, // file 0..9
		{Id: "s2", Start: 0, End: 9, Size: 10}, // file 10..19
		{Id: "s3", Start: 0, End: 9, Size: 10}, // file 20..29
	}, groups: [][]string{{}, {}, {}}}

	rg1 := GetSegmentsInRange(context.Background(), 5, 24, loader)
	rg2 := GetSegmentsInRangeFromIndex(context.Background(), 5, 24, loader, 0, 0)

	if len(rg1.segments) != len(rg2.segments) {
		t.Fatalf("segment count mismatch: %d vs %d", len(rg1.segments), len(rg2.segments))
	}
	for i := range rg1.segments {
		if rg1.segments[i].Id != rg2.segments[i].Id ||
			rg1.segments[i].Start != rg2.segments[i].Start ||
			rg1.segments[i].End != rg2.segments[i].End {
			t.Fatalf("segment %d mismatch", i)
		}
	}
}

func TestGetSegmentsInRangeFromIndex_NegativeIndex(t *testing.T) {
	// Test that negative start index is handled gracefully (defaults to 0)
	loader := &mockLoader{segments: []Segment{
		{Id: "s1", Start: 0, End: 9, Size: 10},
	}, groups: [][]string{{}}}

	rg := GetSegmentsInRangeFromIndex(context.Background(), 0, 9, loader, -5, 0)
	if len(rg.segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(rg.segments))
	}
	if rg.segments[0].Id != "s1" {
		t.Fatalf("expected s1, got %s", rg.segments[0].Id)
	}
}

func TestGetSegmentsInRangeFromIndex_LargeSkip(t *testing.T) {
	// Simulate a large file with many segments to verify O(1) skip works
	const numSegments = 1000
	const segmentSize = 1000
	segments := make([]Segment, numSegments)
	groups := make([][]string, numSegments)
	for i := range segments {
		segments[i] = Segment{Id: string(rune(i)), Start: 0, End: segmentSize - 1, Size: segmentSize}
		groups[i] = []string{}
	}
	loader := &mockLoader{segments: segments, groups: groups}

	// Request bytes from segment 900 (offset 900000)
	// Skip directly to segment 900 instead of iterating through 900 segments
	startOffset := int64(900 * segmentSize)
	rg := GetSegmentsInRangeFromIndex(context.Background(), startOffset, startOffset+999, loader, 900, startOffset)

	if len(rg.segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(rg.segments))
	}
	if collectedLen(rg) != 1000 {
		t.Fatalf("length mismatch got %d want 1000", collectedLen(rg))
	}
}
