package nzbfilesystem

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
	"github.com/javi11/altmount/internal/utils"
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

// An open-ended WebDAV range on a large file is playback: the window opens
// fully rather than ramping.
func TestOpenEndedRangeSkipsRamp(t *testing.T) {
	ctx := context.WithValue(context.Background(), utils.RangeKey, "bytes=0-")
	const n, segSize, maxPrefetch = 3000, 64 << 10, 16 // ~188 MB
	fp := fakepool.New()
	gate := make(chan struct{})
	configurePoolForFile(fp, n, segSize, fakepool.SegmentBehavior{})
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), ChunkSize: 4096, TailGate: gate,
	})
	mvf := newTestMVF(t, ctx, fp, n, segSize, maxPrefetch)
	buf := make([]byte, 1024)
	if _, err := mvf.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	// Segment 0's tail is still held; the manager keeps scheduling the rest of
	// the window in the background, so give it a moment to get there.
	deadline := time.Now().Add(2 * time.Second)
	for fp.BodyPriorityCalls() < maxPrefetch && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := fp.BodyPriorityCalls(); got < maxPrefetch {
		t.Fatalf("open-ended range must open the full window: %d fetches, want %d", got, maxPrefetch)
	}
	close(gate)
}

// Without a range (FUSE) the first read schedules only the base window.
func TestNoRangeRamps(t *testing.T) {
	ctx := context.Background()
	const n, segSize, maxPrefetch = 3000, 64 << 10, 16
	fp := fakepool.New()
	gate := make(chan struct{})
	configurePoolForFile(fp, n, segSize, fakepool.SegmentBehavior{})
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), ChunkSize: 4096, TailGate: gate,
	})
	mvf := newTestMVF(t, ctx, fp, n, segSize, maxPrefetch)
	buf := make([]byte, 1024)
	if _, err := mvf.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // the manager must not schedule more even given time
	if got := fp.BodyPriorityCalls(); got > 16 {
		t.Fatalf("a 1 KB probe scheduled %d fetches, want at most the opening window of 16", got)
	}
	close(gate)
}
