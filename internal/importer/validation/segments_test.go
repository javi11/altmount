package validation

import (
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// segmentsOfSize builds n segments each `size` bytes, with contiguous offsets.
func segmentsOfSize(prefix string, n int, size int64) []*metapb.SegmentData {
	segs := make([]*metapb.SegmentData, n)
	var off int64
	for i := range n {
		segs[i] = &metapb.SegmentData{
			Id:          prefix + "-" + string(rune('0'+i)),
			StartOffset: off,
			EndOffset:   off + size - 1,
			SegmentSize: size,
		}
		off += size
	}
	return segs
}

func TestValidateSegmentsForFile_ValidSegmentsNoError(t *testing.T) {
	segs := segmentsOfSize("seg", 8, 1000) // total 8000 bytes
	err := ValidateSegmentsForFile("movie.mkv", 8000, segs, metapb.Encryption_NONE)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateSegmentsForFile_DetectsIncompleteFile(t *testing.T) {
	segs := segmentsOfSize("seg", 8, 1000) // total 8000 bytes

	// Declared file size larger than segment sum → incomplete; must error
	err := ValidateSegmentsForFile("movie.mkv", 9000, segs, metapb.Encryption_NONE)
	if err == nil {
		t.Fatal("expected incomplete-file error, got nil")
	}
}

func TestValidateSegmentsForFile_EmptySegmentsError(t *testing.T) {
	err := ValidateSegmentsForFile("movie.mkv", 1000, nil, metapb.Encryption_NONE)
	if err == nil {
		t.Fatal("expected error for empty segments, got nil")
	}
}
