package usenet

import (
	"context"
	"testing"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// streamtest_helpers_test.go is the test-only construction kit shared by all
// streaming/connection invariant tests in this package. Keeping it inside
// internal/usenet (rather than internal/testsupport) is deliberate: the
// segment and segmentRange types are unexported, and exposing them would
// invite production code to depend on the test shape.

// noopMetrics is a MetricsTracker that satisfies the interface without any
// state. Used by tests that don't care about metrics observation — most do
// not, since the in-flight counter on the fake pool is the primary signal.
type noopMetrics struct{}

func (noopMetrics) IncArticlesDownloaded()                   {}
func (noopMetrics) IncArticlesPosted()                       {}
func (noopMetrics) UpdateDownloadProgress(_ string, _ int64) {}

// buildEagerRange creates a segmentRange backed by an in-memory slice of
// segments. Each segment has Id=segments.MessageID(i), spans [0, size-1]
// within itself, and reports SegmentSize=size. The range covers the
// concatenated byte stream of all segments.
//
// "Eager" means no SegmentLoader is attached, so the segments slice must
// already be populated — which matches the simplest path through
// downloadManager and avoids dragging metadata-layer concerns into a pure
// streaming test.
func buildEagerRange(ctx context.Context, t testing.TB, n, segSize int) *segmentRange {
	t.Helper()
	segs := make([]*segment, n)
	for i := range segs {
		segs[i] = newSegment(segments.MessageID(i), 0, int64(segSize-1), int64(segSize), nil)
	}
	return &segmentRange{
		segments: segs,
		start:    0,
		end:      int64(n*segSize - 1),
		ctx:      ctx,
	}
}

// newReaderForTest constructs a UsenetReader wired to the supplied fake
// pool. The maxPrefetch parameter mirrors production semantics: it bounds
// how many segments the downloadManager may schedule ahead of the current
// read position.
func newReaderForTest(t testing.TB, ctx context.Context, fp *fakepool.Client, rg *segmentRange, maxPrefetch int) *UsenetReader {
	t.Helper()
	return newReaderForTestWithClient(t, ctx, fp, rg, maxPrefetch)
}

// newReaderForTestWithClient is the lower-level constructor used when the
// test needs to inject a wrapper around the fake (e.g. a recording client
// that timestamps calls). The supplied client must satisfy pool.NntpClient.
func newReaderForTestWithClient(t testing.TB, ctx context.Context, cp pool.NntpClient, rg *segmentRange, maxPrefetch int) *UsenetReader {
	t.Helper()
	getter := func() (pool.NntpClient, error) { return cp, nil }
	ur, err := NewUsenetReader(ctx, getter, rg, maxPrefetch, noopMetrics{}, "test-stream", nil)
	if err != nil {
		t.Fatalf("NewUsenetReader: %v", err)
	}
	t.Cleanup(func() { _ = ur.Close() })
	return ur
}
