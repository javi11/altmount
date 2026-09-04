package usenet

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

func rampPool(n, segSize int, latency time.Duration) *fakepool.Client {
	fp := fakepool.New()
	for i := 0; i < n; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{Bytes: segments.Payload(i, segSize), Latency: latency})
	}
	return fp
}

func newRampReader(t *testing.T, ctx context.Context, fp *fakepool.Client, rg *segmentRange, maxPrefetch int, opts ...ReaderOption) *UsenetReader {
	t.Helper()
	getter := func() (pool.NntpClient, error) { return fp, nil }
	ur, err := NewUsenetReader(ctx, getter, rg, maxPrefetch, noopMetrics{}, "ramp-test", nil, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ur.Close() })
	return ur
}

// A fresh reader without streaming intent opens only a slip of read-ahead
// until the caller consumes segments.
func TestFreshReaderRampsFromABaseWindow(t *testing.T) {
	ctx := context.Background()
	const nSegs, segSize, maxPrefetch = 40, 256, 60
	fp := rampPool(nSegs, segSize, 0)
	gate := make(chan struct{})
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), ChunkSize: 64, TailGate: gate,
	})
	rg := buildEagerRange(ctx, t, nSegs, segSize)
	ur := newRampReader(t, ctx, fp, rg, maxPrefetch)
	ur.Start()

	time.Sleep(100 * time.Millisecond) // let the manager schedule what it will
	if got := fp.BodyPriorityCalls(); got > rampBaseSegments {
		t.Fatalf("scheduled %d fetches before any byte was consumed, want <= %d", got, rampBaseSegments)
	}
	if got := ur.BufferedAhead(); got != int64(rampBaseSegments*segSize) {
		t.Fatalf("BufferedAhead = %d, want %d", got, rampBaseSegments*segSize)
	}

	close(gate)
	got, err := io.ReadAll(ur)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, segments.FileBytes(nSegs, segSize)) {
		t.Fatal("payload differs")
	}
	if fp.MaxInFlight() <= int32(rampBaseSegments) {
		t.Fatalf("window never grew past the base: max in flight %d", fp.MaxInFlight())
	}
	if ur.BufferedAhead() != 0 {
		t.Fatalf("BufferedAhead after full read = %d", ur.BufferedAhead())
	}
}

// A range hint at or above the streaming-intent threshold opens the window
// fully at once.
func TestStreamSizedRangeHintSkipsTheRamp(t *testing.T) {
	ctx := context.Background()
	const nSegs, segSize, maxPrefetch = 30, 128, 12
	fp := rampPool(nSegs, segSize, 30*time.Millisecond)
	rg := buildEagerRange(ctx, t, nSegs, segSize)
	ur := newRampReader(t, ctx, fp, rg, maxPrefetch, WithRangeHint(streamingIntentBytes))
	ur.Start()
	if _, err := io.ReadAll(ur); err != nil {
		t.Fatal(err)
	}
	if fp.MaxInFlight() != int32(maxPrefetch) {
		t.Fatalf("max in flight = %d, want the full window %d", fp.MaxInFlight(), maxPrefetch)
	}
}

// Import readers read whole files and keep the full window from the start.
func TestImportReaderDoesNotRamp(t *testing.T) {
	ctx := context.Background()
	const nSegs, segSize, maxPrefetch = 30, 128, 8
	fp := rampPool(nSegs, segSize, 30*time.Millisecond)
	rg := buildEagerRange(ctx, t, nSegs, segSize)
	ur := newRampReader(t, ctx, fp, rg, maxPrefetch, WithImportProfile(nil))
	ur.Start()
	if _, err := io.ReadAll(ur); err != nil {
		t.Fatal(err)
	}
	if fp.MaxInFlight() != int32(maxPrefetch) {
		t.Fatalf("import max in flight = %d, want %d", fp.MaxInFlight(), maxPrefetch)
	}
}
