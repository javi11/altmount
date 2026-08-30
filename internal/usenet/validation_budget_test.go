package usenet

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/nntppool/v4"
)

// budgetedPoolManager admits sweeps through a real StatBudget and records the
// Concurrency every StatMany wave asked for.
type budgetedPoolManager struct {
	*validationTestPoolManager
	budget *pool.StatBudget

	mu    sync.Mutex
	waves []int
}

func (m *budgetedPoolManager) AcquireStatSlots(ctx context.Context, want int) (int, func(), error) {
	return m.budget.Acquire(ctx, want)
}

func (m *budgetedPoolManager) SetStatCapacity(n int) { m.budget.SetCapacity(n) }

func (m *budgetedPoolManager) GetPool() (pool.NntpClient, error) {
	inner, err := m.validationTestPoolManager.GetPool()
	if err != nil {
		return nil, err
	}
	return &waveRecordingClient{NntpClient: inner, mgr: m}, nil
}

type waveRecordingClient struct {
	pool.NntpClient
	mgr *budgetedPoolManager
}

func (c *waveRecordingClient) StatMany(ctx context.Context, ids []string, opts nntppool.StatManyOptions) <-chan nntppool.StatManyResult {
	c.mgr.mu.Lock()
	c.mgr.waves = append(c.mgr.waves, opts.Concurrency)
	c.mgr.mu.Unlock()
	return c.NntpClient.StatMany(ctx, ids, opts)
}

// The health sweep must run at the concurrency the shared budget grants, not
// the one it asked for. MaxConnections is the providers' connection count;
// the budget is the pool's STAT pipeline depth, and import verification is
// drawing on it at the same time.
func TestValidateSegmentAvailabilityBatchRunsAtGrantedConcurrency(t *testing.T) {
	client := fakepool.New()
	mgr := &budgetedPoolManager{
		validationTestPoolManager: &validationTestPoolManager{client: client},
		budget:                    pool.NewStatBudget(),
	}
	mgr.SetStatCapacity(4)

	_, err := ValidateSegmentAvailabilityBatch(
		context.Background(),
		[][]string{idList("seg", 40)},
		mgr,
		BatchOptions{MaxConnections: 100, Timeout: time.Second},
	)
	if err != nil {
		t.Fatalf("ValidateSegmentAvailabilityBatch: %v", err)
	}

	mgr.mu.Lock()
	waves := append([]int(nil), mgr.waves...)
	mgr.mu.Unlock()

	if len(waves) == 0 {
		t.Fatal("no StatMany waves were issued")
	}
	for i, c := range waves {
		if c > 4 {
			t.Fatalf("wave %d ran at concurrency %d, want <= 4 (the budget grant); waves=%v", i, c, waves)
		}
	}
}

// concurrencyTracker records the high-water mark of STAT concurrency handed to
// StatMany while calls overlap, which is the quantity the shared budget bounds.
type concurrencyTracker struct {
	pool.NntpClient
	// overlap is closed once two StatMany calls are in flight together, and
	// barrier is the one-shot wait for that to happen. Without forcing overlap
	// the fake pool answers so fast that the two sweeps interleave instead of
	// running together, and the test passes whether or not a shared cap exists.
	overlap     chan struct{}
	overlapOnce sync.Once
	barrierOnce sync.Once

	mu       sync.Mutex
	inFlight int
	peak     int
}

func (c *concurrencyTracker) StatMany(ctx context.Context, ids []string, opts nntppool.StatManyOptions) <-chan nntppool.StatManyResult {
	c.mu.Lock()
	c.inFlight += opts.Concurrency
	if c.inFlight > c.peak {
		c.peak = c.inFlight
	}
	overlapping := c.inFlight > opts.Concurrency
	c.mu.Unlock()

	if overlapping {
		c.overlapOnce.Do(func() { close(c.overlap) })
	} else {
		// Wait once for a second sweep to join. When the budget is doing its
		// job that sweep is still queued for admission and never arrives, so
		// give up after a moment and let the sweep proceed.
		c.barrierOnce.Do(func() {
			select {
			case <-c.overlap:
			case <-time.After(500 * time.Millisecond):
			}
		})
	}

	inner := c.NntpClient.StatMany(ctx, ids, opts)
	out := make(chan nntppool.StatManyResult)
	go func() {
		defer close(out)
		defer func() {
			c.mu.Lock()
			c.inFlight -= opts.Concurrency
			c.mu.Unlock()
		}()
		for r := range inner {
			out <- r
		}
	}()
	return out
}

type trackingPoolManager struct {
	*validationTestPoolManager
	budget  *pool.StatBudget
	tracker *concurrencyTracker
}

func (m *trackingPoolManager) AcquireStatSlots(ctx context.Context, want int) (int, func(), error) {
	return m.budget.Acquire(ctx, want)
}

func (m *trackingPoolManager) GetPool() (pool.NntpClient, error) { return m.tracker, nil }

// This is the #862 regression. Health monitoring and import verification each
// enforced their own STAT concurrency against the same pool, so their combined
// outstanding checks reached the sum of both limits. Each sweep's deadline is
// sized as if it owned the concurrency it asked for, so the excess queued
// inside the pool, burned its own deadline, and came back as unresolved
// segments — a busy pool reported healthy articles as unavailable.
func TestConcurrentSweepsShareOneStatBudget(t *testing.T) {
	client := fakepool.New()
	inner := &validationTestPoolManager{client: client}
	raw, err := inner.GetPool()
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}

	const capacity = 8
	mgr := &trackingPoolManager{
		validationTestPoolManager: inner,
		budget:                    pool.NewStatBudget(),
		tracker:                   &concurrencyTracker{NntpClient: raw, overlap: make(chan struct{})},
	}
	mgr.budget.SetCapacity(capacity)

	var wg sync.WaitGroup
	for sweep := range 2 {
		wg.Add(1)
		go func(sweep int) {
			defer wg.Done()
			ids := idList(fmt.Sprintf("sweep%d", sweep), 200)
			if _, err := ValidateSegmentAvailabilityBatch(
				context.Background(), [][]string{ids}, mgr,
				BatchOptions{MaxConnections: capacity, Timeout: time.Second},
			); err != nil {
				t.Errorf("sweep %d: %v", sweep, err)
			}
		}(sweep)
	}
	wg.Wait()

	mgr.tracker.mu.Lock()
	peak := mgr.tracker.peak
	mgr.tracker.mu.Unlock()

	if peak > capacity {
		t.Fatalf("peak concurrent STAT checks = %d, want <= %d", peak, capacity)
	}
}
