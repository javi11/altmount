package usenet

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// TestSequentialRead_OneRequestPerSegment pins the simplest correctness
// contract for the streaming pipeline: reading a file end-to-end issues
// exactly one BodyPriority per segment, and the reassembled bytes match
// what the fake pool returned.
//
// This is the moral equivalent of "sequential ReadAt reuses the shared
// reader" at the MetadataVirtualFile layer: any code path that double-
// fetches a segment, or that creates two readers for overlapping regions
// of the same stream, will trip both the per-message-count assertion and
// the byte-equality assertion below. Pinning this here keeps the
// invariant testable without dragging the metadata layer into a unit
// test.
//
// Should pass on current code.
func TestSequentialRead_OneRequestPerSegment(t *testing.T) {
	t.Parallel()
	const (
		segCount    = 8
		segSize     = 128
		maxPrefetch = 4
	)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fp := fakepool.New()
	for i := 0; i < segCount; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{
			Bytes: segments.Payload(i, segSize),
		})
	}

	rg := buildEagerRange(ctx, t, segCount, segSize)
	ur := newReaderForTest(t, ctx, fp, rg, maxPrefetch)
	ur.Start()

	got, err := io.ReadAll(ur)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	want := segments.FileBytes(segCount, segSize)
	if !bytes.Equal(got, want) {
		t.Errorf("reassembled bytes do not match expected payload (len got=%d, want=%d)",
			len(got), len(want))
	}

	for i := 0; i < segCount; i++ {
		if c := fp.PerMessageCalls(segments.MessageID(i)); c != 1 {
			t.Errorf("segment %d: %d BodyPriority calls, want exactly 1", i, c)
		}
	}
}
