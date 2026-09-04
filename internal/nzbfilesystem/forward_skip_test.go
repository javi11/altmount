package nzbfilesystem

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// A forward jump past what the shared reader has buffered reopens at the
// target instead of downloading every article in the gap.
func TestForwardSkipBeyondBufferReopensAtTarget(t *testing.T) {
	ctx := context.Background()
	const n, segSize, maxPrefetch = 200, 64 << 10, 4
	fp := fakepool.New()
	configurePoolForFile(fp, n, segSize, fakepool.SegmentBehavior{})
	mvf := newTestMVF(t, ctx, fp, n, segSize, maxPrefetch)
	want := segments.FileBytes(n, segSize)

	buf := make([]byte, segSize)
	if _, err := mvf.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, want[:segSize]) {
		t.Fatal("head bytes differ")
	}
	jump := int64(40 * segSize) // 2.5 MB: inside forwardSkipLimit, far outside the 4-segment window
	if _, err := mvf.ReadAt(buf, jump); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, want[jump:jump+segSize]) {
		t.Fatal("jump bytes differ")
	}
	if got := fp.BodyPriorityCalls(); got > 12 {
		t.Fatalf("fetched %d articles for a head read and one jump; the gap was drained instead of skipped", got)
	}
}

// A small forward skip inside the buffered window keeps the shared reader.
func TestForwardSkipInsideBufferKeepsReader(t *testing.T) {
	ctx := context.Background()
	const n, segSize, maxPrefetch = 50, 64 << 10, 10
	fp := fakepool.New()
	configurePoolForFile(fp, n, segSize, fakepool.SegmentBehavior{})
	mvf := newTestMVF(t, ctx, fp, n, segSize, maxPrefetch)
	want := segments.FileBytes(n, segSize)

	buf := make([]byte, 3*segSize) // consume three segments so the window has opened
	if _, err := mvf.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	mvf.mu.Lock()
	before := mvf.reader
	mvf.mu.Unlock()

	jump := int64(4 * segSize) // one segment past the read position
	small := make([]byte, segSize)
	if _, err := mvf.ReadAt(small, jump); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(small, want[jump:jump+segSize]) {
		t.Fatal("bytes differ")
	}
	mvf.mu.Lock()
	after := mvf.reader
	mvf.mu.Unlock()
	if before != after {
		t.Fatal("a skip inside the buffered window must drain through the shared reader")
	}
}

// Before the first byte of a head read arrives, a fresh handle has fanned
// out no more than the opening window.
func TestHeadReadFansOutAtMostTheOpeningWindowBeforeFirstByte(t *testing.T) {
	ctx := context.Background()
	const n, segSize, maxPrefetch = 3000, 64 << 10, 60
	fp := fakepool.New()
	configurePoolForFile(fp, n, segSize, fakepool.SegmentBehavior{})
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), Latency: 500 * time.Millisecond,
	})
	mvf := newTestMVF(t, ctx, fp, n, segSize, maxPrefetch)
	buf := make([]byte, 1024)
	done := make(chan struct{})
	go func() { _, _ = mvf.ReadAt(buf, 0); close(done) }()
	time.Sleep(150 * time.Millisecond) // segment 0 has not delivered a byte yet
	if got := fp.BodyPriorityCalls(); got > 16 {
		t.Fatalf("fanned out %d fetches before the first byte, want at most 16", got)
	}
	<-done
}
