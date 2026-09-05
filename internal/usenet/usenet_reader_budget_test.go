package usenet

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// countingSpecBudget is a SpecBudget with a fixed capacity that records the
// high-water mark of concurrently held slots.
type countingSpecBudget struct {
	mu       sync.Mutex
	cap      int
	held     int
	maxHeld  int
	refused  atomic.Int64
	acquired atomic.Int64
}

func (b *countingSpecBudget) TryAcquire() (func(), bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.held >= b.cap {
		b.refused.Add(1)
		return nil, false
	}
	b.held++
	b.acquired.Add(1)
	if b.held > b.maxHeld {
		b.maxHeld = b.held
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			b.held--
			b.mu.Unlock()
		})
	}, true
}

func (b *countingSpecBudget) max() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxHeld
}

func newBudgetReader(t *testing.T, ctx context.Context, fp *fakepool.Client, rg *segmentRange, maxPrefetch int, budget SpecBudget) *UsenetReader {
	t.Helper()
	getter := func() (pool.NntpClient, error) { return fp, nil }
	ur, err := NewUsenetReader(ctx, getter, rg, maxPrefetch, noopMetrics{}, "budget-test", nil, WithSpeculativeBudget(budget), withFlightMap(newFlightMap()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ur.Close() })
	return ur
}

// Speculative fetches never exceed the budget, while the demand segments at
// the read position start regardless.
func TestSpeculativeFetchesRespectBudget(t *testing.T) {
	ctx := context.Background()
	const nSegs, segSize, maxPrefetch = 20, 256, 12
	fp := fakepool.New()
	for i := 0; i < nSegs; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{Bytes: segments.Payload(i, segSize), Latency: 20 * time.Millisecond})
	}
	budget := &countingSpecBudget{cap: 3}
	rg := buildEagerRange(ctx, t, nSegs, segSize)
	ur := newBudgetReader(t, ctx, fp, rg, maxPrefetch, budget)
	ur.Start()

	got, err := io.ReadAll(ur)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != nSegs*segSize {
		t.Fatalf("read %d bytes, want %d", len(got), nSegs*segSize)
	}
	if budget.max() > 3 {
		t.Fatalf("speculative in flight reached %d, budget is 3", budget.max())
	}
	if budget.acquired.Load() == 0 {
		t.Fatal("speculative fetches must take budget slots")
	}
	// Demand fetches (demandDepth per position) are outside the budget, so at
	// most demandDepth + cap fetches overlap.
	fakepool.AssertMaxInFlightLE(t, fp, int32(demandDepth+3))
}

// With the budget fully held elsewhere, a sequential read still completes
// through demand fetches alone.
func TestExhaustedBudgetNeverBlocksDemandReads(t *testing.T) {
	ctx := context.Background()
	const nSegs, segSize = 8, 128
	fp := fakepool.New()
	for i := 0; i < nSegs; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{Bytes: segments.Payload(i, segSize)})
	}
	budget := &countingSpecBudget{cap: 0}
	rg := buildEagerRange(ctx, t, nSegs, segSize)
	ur := newBudgetReader(t, ctx, fp, rg, 6, budget)
	ur.Start()

	done := make(chan error, 1)
	go func() {
		got, err := io.ReadAll(ur)
		if err == nil && len(got) != nSegs*segSize {
			err = io.ErrUnexpectedEOF
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read stalled with the speculative budget exhausted")
	}
	if budget.acquired.Load() != 0 {
		t.Fatal("no slot was available, nothing may have been acquired")
	}
}

// No budget means the previous behaviour: every segment fetched on the
// priority lane up to maxPrefetch ahead.
func TestNilBudgetKeepsFullWindow(t *testing.T) {
	ctx := context.Background()
	const nSegs, segSize, maxPrefetch = 10, 64, 4
	fp := fakepool.New()
	for i := 0; i < nSegs; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{Bytes: segments.Payload(i, segSize), Latency: 10 * time.Millisecond})
	}
	rg := buildEagerRange(ctx, t, nSegs, segSize)
	ur := newBudgetReader(t, ctx, fp, rg, maxPrefetch, nil)
	ur.Start()
	if _, err := io.ReadAll(ur); err != nil {
		t.Fatal(err)
	}
	if fp.BodyPriorityCalls() != nSegs {
		t.Fatalf("priority calls = %d, want %d", fp.BodyPriorityCalls(), nSegs)
	}
	fakepool.AssertMaxInFlightLE(t, fp, int32(maxPrefetch))
}
