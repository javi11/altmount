package usenet

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
)

// usenet_reader_lane_test.go pins the lane and budget invariants introduced by
// automatic import connection balancing:
//   - streaming readers (default) fetch via BodyPriority (priority lane);
//   - import readers (WithImportProfile) fetch via Body (normal lane) and
//     bracket every fetch with an import connection budget token.

// recordingBudget counts acquire/release pairs and tracks the in-flight high
// water mark.
type recordingBudget struct {
	acquires    atomic.Int64
	inFlight    atomic.Int64
	maxInFlight atomic.Int64
}

func (b *recordingBudget) AcquireImportConnection(_ context.Context) (func(), error) {
	b.acquires.Add(1)
	n := b.inFlight.Add(1)
	for {
		old := b.maxInFlight.Load()
		if n <= old || b.maxInFlight.CompareAndSwap(old, n) {
			break
		}
	}
	return func() { b.inFlight.Add(-1) }, nil
}

func drainReader(t *testing.T, ur *UsenetReader) {
	t.Helper()
	if _, err := io.Copy(io.Discard, ur); err != nil {
		t.Fatalf("read: %v", err)
	}
}

func TestUsenetReader_StreamingUsesPriorityLane(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const nSegs, segSize = 4, 64
	fp := fakepool.New()
	rg := buildEagerRange(ctx, t, nSegs, segSize)
	ur := newReaderForTest(t, ctx, fp, rg, nSegs)

	drainReader(t, ur)

	if got := fp.BodyPriorityCalls(); got != nSegs {
		t.Errorf("BodyPriorityCalls = %d, want %d (streaming must use the priority lane)", got, nSegs)
	}
	if got := fp.BodyCalls(); got != 0 {
		t.Errorf("BodyCalls = %d, want 0 (streaming must not use the normal lane)", got)
	}
}

func TestUsenetReader_ImportProfileUsesNormalLaneAndBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const nSegs, segSize = 4, 64
	fp := fakepool.New()
	rg := buildEagerRange(ctx, t, nSegs, segSize)

	budget := &recordingBudget{}
	getter := func() (pool.NntpClient, error) { return fp, nil }
	ur, err := NewUsenetReader(ctx, getter, rg, nSegs, noopMetrics{}, "test-import", nil,
		WithImportProfile(budget))
	if err != nil {
		t.Fatalf("NewUsenetReader: %v", err)
	}
	t.Cleanup(func() { _ = ur.Close() })

	drainReader(t, ur)

	if got := fp.BodyCalls(); got != nSegs {
		t.Errorf("BodyCalls = %d, want %d (import must use the normal lane)", got, nSegs)
	}
	if got := fp.BodyPriorityCalls(); got != 0 {
		t.Errorf("BodyPriorityCalls = %d, want 0 (import must not use the priority lane)", got)
	}
	if got := budget.acquires.Load(); got != nSegs {
		t.Errorf("budget acquires = %d, want %d (every fetch must take a token)", got, nSegs)
	}
	if got := budget.inFlight.Load(); got != 0 {
		t.Errorf("budget in-flight after drain = %d, want 0 (every token must be released)", got)
	}
}

func TestUsenetReader_ImportBudgetBoundsInFlightFetches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const nSegs, segSize = 12, 64
	fp := fakepool.New()
	rg := buildEagerRange(ctx, t, nSegs, segSize)

	// Real budget with capacity 2: the reader's prefetch fan-out must never
	// exceed it on the wire.
	budget := pool.NewImportBudget()
	budget.SetCapacity(2)
	getter := func() (pool.NntpClient, error) { return fp, nil }
	ur, err := NewUsenetReader(ctx, getter, rg, nSegs, noopMetrics{}, "test-import", nil,
		WithImportProfile(budgetAdapter{budget}))
	if err != nil {
		t.Fatalf("NewUsenetReader: %v", err)
	}
	t.Cleanup(func() { _ = ur.Close() })

	drainReader(t, ur)

	if mif := fp.MaxInFlight(); mif > 2 {
		t.Errorf("MaxInFlight = %d, want <= 2 (budget capacity)", mif)
	}
}

// budgetAdapter exposes pool.ImportBudget through the reader's ConnBudget
// interface, mirroring how pool.Manager delegates to its budget.
type budgetAdapter struct{ b *pool.ImportBudget }

func (a budgetAdapter) AcquireImportConnection(ctx context.Context) (func(), error) {
	return a.b.Acquire(ctx)
}
