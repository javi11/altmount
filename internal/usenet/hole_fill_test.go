package usenet

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/javi11/altmount/internal/holes"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
	"github.com/javi11/nntppool/v4"
)

// newReaderWithHooks builds a reader wired to the fake pool with the given
// hole hooks.
func newReaderWithHooks(t testing.TB, ctx context.Context, fp *fakepool.Client, rg *segmentRange, maxPrefetch int, hooks *HoleHooks) *UsenetReader {
	t.Helper()
	getter := func() (pool.NntpClient, error) { return fp, nil }
	ur, err := NewUsenetReader(ctx, getter, rg, maxPrefetch, noopMetrics{}, "test-stream", nil, WithHoleHooks(hooks))
	if err != nil {
		t.Fatalf("NewUsenetReader: %v", err)
	}
	t.Cleanup(func() { _ = ur.Close() })
	return ur
}

// fillFakePool returns a fake pool where every segment serves a byte equal to
// its index+1 (so zero-filled gaps are distinguishable from real data).
func fillFakePool(n, segSize int) *fakepool.Client {
	fp := fakepool.New()
	for i := 0; i < n; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{
			Bytes: bytes.Repeat([]byte{byte(i + 1)}, segSize),
		})
	}
	return fp
}

func TestReaderZeroFillsApprovedHole(t *testing.T) {
	ctx := context.Background()
	const n, segSize = 8, 4
	fp := fillFakePool(n, segSize)
	fp.SetBehavior(segments.MessageID(3), fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	var onHoleCalls int32
	hooks := &HoleHooks{
		OnHole: func(segIndex int, segID string) holes.Decision {
			atomic.AddInt32(&onHoleCalls, 1)
			if segIndex == 3 {
				return holes.DecisionPad
			}
			return holes.DecisionFail
		},
	}

	rg := buildEagerRange(ctx, t, n, segSize)
	ur := newReaderWithHooks(t, ctx, fp, rg, 60, hooks)

	got, err := io.ReadAll(ur)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != n*segSize {
		t.Fatalf("read %d bytes, want %d", len(got), n*segSize)
	}
	// Segment 3's window must be exactly zeros; neighbors must be intact.
	for i := 0; i < n; i++ {
		want := byte(i + 1)
		if i == 3 {
			want = 0
		}
		for j := 0; j < segSize; j++ {
			off := i*segSize + j
			if got[off] != want {
				t.Fatalf("byte %d (segment %d) = %d, want %d", off, i, got[off], want)
			}
		}
	}
	if n := atomic.LoadInt32(&onHoleCalls); n != 1 {
		t.Fatalf("OnHole called %d times, want 1", n)
	}
}

func TestReaderFailsUnapprovedHole(t *testing.T) {
	ctx := context.Background()
	const n, segSize = 8, 4
	fp := fillFakePool(n, segSize)
	fp.SetBehavior(segments.MessageID(3), fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	hooks := &HoleHooks{
		OnHole: func(segIndex int, segID string) holes.Decision { return holes.DecisionFail },
	}

	rg := buildEagerRange(ctx, t, n, segSize)
	ur := newReaderWithHooks(t, ctx, fp, rg, 60, hooks)

	_, err := io.ReadAll(ur)
	var dcErr *DataCorruptionError
	if !errors.As(err, &dcErr) {
		t.Fatalf("expected DataCorruptionError, got %v", err)
	}
}

func TestReaderNilHooksFailsHole(t *testing.T) {
	ctx := context.Background()
	const n, segSize = 8, 4
	fp := fillFakePool(n, segSize)
	fp.SetBehavior(segments.MessageID(3), fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	rg := buildEagerRange(ctx, t, n, segSize)
	ur := newReaderForTest(t, ctx, fp, rg, 60) // no hooks

	_, err := io.ReadAll(ur)
	var dcErr *DataCorruptionError
	if !errors.As(err, &dcErr) {
		t.Fatalf("expected DataCorruptionError with nil hooks, got %v", err)
	}
}

func TestReaderKnownHolePrePadsWithoutFetch(t *testing.T) {
	ctx := context.Background()
	const n, segSize = 8, 4
	fp := fillFakePool(n, segSize)
	// Segment 3 would serve real data if fetched — but KnownHoles pre-pads it,
	// so no fetch should occur and the window must be zeros.

	var onHoleCalls int32
	hooks := &HoleHooks{
		OnHole:     func(int, string) holes.Decision { atomic.AddInt32(&onHoleCalls, 1); return holes.DecisionFail },
		KnownHoles: func(segIndex int) bool { return segIndex == 3 },
	}

	rg := buildEagerRange(ctx, t, n, segSize)
	ur := newReaderWithHooks(t, ctx, fp, rg, 60, hooks)

	got, err := io.ReadAll(ur)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	for j := 0; j < segSize; j++ {
		if got[3*segSize+j] != 0 {
			t.Fatalf("known-hole segment 3 byte %d = %d, want 0", j, got[3*segSize+j])
		}
	}
	// A known hole must never fetch, and never consult OnHole.
	if calls := fp.PerMessageCalls(segments.MessageID(3)); calls != 0 {
		t.Fatalf("known hole segment 3 fetched %d times, want 0", calls)
	}
	if n := atomic.LoadInt32(&onHoleCalls); n != 0 {
		t.Fatalf("OnHole called %d times for a known hole, want 0", n)
	}
}
