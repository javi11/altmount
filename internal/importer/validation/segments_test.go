package validation

import (
	"context"
	"testing"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
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

func TestValidateSegmentsForFile_SkipNetworkSkipsStatButKeepsSizeCheck(t *testing.T) {
	client := fakepool.New()
	segs := segmentsOfSize("seg", 8, 1000) // total 8000 bytes

	// Correct size + skipNetwork: no error, no Stat calls.
	err := ValidateSegmentsForFile(
		context.Background(),
		"movie.mkv",
		8000,
		segs,
		metapb.Encryption_NONE,
		fastFailPoolManager{client: client},
		4,
		100,
		nil,
		100*time.Millisecond,
		true, // skipNetworkValidation
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := client.StatCalls(); got != 0 {
		t.Fatalf("StatCalls = %d, want 0 (network validation should be skipped)", got)
	}
}

func TestValidateSegmentsForFile_SkipNetworkStillDetectsIncompleteFile(t *testing.T) {
	client := fakepool.New()
	segs := segmentsOfSize("seg", 8, 1000) // total 8000 bytes

	// Declared file size larger than segment sum → incomplete; must still error
	// even though the network check is skipped.
	err := ValidateSegmentsForFile(
		context.Background(),
		"movie.mkv",
		9000,
		segs,
		metapb.Encryption_NONE,
		fastFailPoolManager{client: client},
		4,
		100,
		nil,
		100*time.Millisecond,
		true, // skipNetworkValidation
	)
	if err == nil {
		t.Fatal("expected incomplete-file error, got nil")
	}
	if got := client.StatCalls(); got != 0 {
		t.Fatalf("StatCalls = %d, want 0", got)
	}
}

func TestValidateSegmentsForFile_NetworkValidationRunsWhenNotSkipped(t *testing.T) {
	client := fakepool.New()
	segs := segmentsOfSize("seg", 8, 1000) // total 8000 bytes

	err := ValidateSegmentsForFile(
		context.Background(),
		"movie.mkv",
		8000,
		segs,
		metapb.Encryption_NONE,
		fastFailPoolManager{client: client},
		4,
		100, // 100% sampling → all 8 segments validated
		nil,
		100*time.Millisecond,
		false, // skipNetworkValidation
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := client.StatCalls(); got == 0 {
		t.Fatal("StatCalls = 0, want network validation to run")
	}
}
