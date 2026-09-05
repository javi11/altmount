package usenet

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// The reader must hand over the head of an article while its tail is
// provably still on the wire. Gate-based so it cannot flake on timing.
func TestReaderServesHeadWhileTailIsHeld(t *testing.T) {
	ctx := context.Background()
	const segSize = 8 << 10
	fp := fakepool.New()
	gate := make(chan struct{})
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), ChunkSize: 1024, TailGate: gate,
	})
	rg := buildEagerRange(ctx, t, 1, segSize)
	ur := newReaderForTest(t, ctx, fp, rg, 1)
	ur.Start()

	buf := make([]byte, 512)
	readDone := make(chan error, 1)
	go func() { _, err := io.ReadFull(ur, buf); readDone <- err }()
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first 512 bytes were not served while the tail was held")
	}
	if !bytes.Equal(buf, segments.Payload(0, segSize)[:512]) {
		t.Fatal("head bytes differ")
	}
	close(gate)
	rest, err := io.ReadAll(ur)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(buf, rest...), segments.Payload(0, segSize)) {
		t.Fatal("full payload differs")
	}
}

// A connection lost after the first chunk must yield the exact payload once
// after the retry, never a duplicated prefix.
func TestReaderRetriesPartialDeliveryWithoutDuplicating(t *testing.T) {
	ctx := context.Background()
	const segSize = 4 << 10
	fp := fakepool.New()
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), ChunkSize: 1024,
		FailAfterFirstChunk: true, FailErr: errors.New("connection reset"),
	})
	rg := buildEagerRange(ctx, t, 1, segSize)
	ur := newReaderForTest(t, ctx, fp, rg, 1)
	ur.Start()
	got, err := io.ReadAll(ur)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, segments.Payload(0, segSize)) {
		t.Fatalf("got %d bytes, want %d exact", len(got), segSize)
	}
	if fp.BodyStreamPriorityCalls() != 2 {
		t.Fatalf("calls = %d, want 2 (one failed, one retry)", fp.BodyStreamPriorityCalls())
	}
}

// Import readers keep the buffered path.
func TestImportReaderStillUsesBufferedBody(t *testing.T) {
	ctx := context.Background()
	fp := fakepool.New()
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{Bytes: segments.Payload(0, 1024)})
	rg := buildEagerRange(ctx, t, 1, 1024)
	getter := func() (pool.NntpClient, error) { return fp, nil }
	ur, err := NewUsenetReader(ctx, getter, rg, 1, noopMetrics{}, "import", nil, WithImportProfile(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ur.Close() })
	ur.Start()
	if _, err := io.ReadAll(ur); err != nil {
		t.Fatal(err)
	}
	if fp.BodyCalls() != 1 || fp.BodyStreamPriorityCalls() != 0 {
		t.Fatalf("import must use Body: body=%d stream=%d", fp.BodyCalls(), fp.BodyStreamPriorityCalls())
	}
}
