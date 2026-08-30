package validation

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

// budgetedFastFailManager admits sweeps through a real StatBudget and logs
// admission and Stat passes in order.
type budgetedFastFailManager struct {
	fastFailPoolManager
	budget *pool.StatBudget

	mu     sync.Mutex
	events []string
	waves  []int
}

func (m *budgetedFastFailManager) log(e string) {
	m.mu.Lock()
	m.events = append(m.events, e)
	m.mu.Unlock()
}

func (m *budgetedFastFailManager) AcquireStatSlots(ctx context.Context, want int) (int, func(), error) {
	granted, release, err := m.budget.Acquire(ctx, want)
	if err != nil {
		return 0, release, err
	}
	m.log("acquire")
	return granted, func() { m.log("release"); release() }, nil
}

func (m *budgetedFastFailManager) GetPool() (pool.NntpClient, error) {
	inner, err := m.fastFailPoolManager.GetPool()
	if err != nil {
		return nil, err
	}
	return &budgetedFastFailClient{NntpClient: inner, mgr: m}, nil
}

type budgetedFastFailClient struct {
	pool.NntpClient
	mgr *budgetedFastFailManager
}

func (c *budgetedFastFailClient) StatMany(ctx context.Context, ids []string, opts nntppool.StatManyOptions) <-chan nntppool.StatManyResult {
	c.mgr.mu.Lock()
	c.mgr.events = append(c.mgr.events, "stat")
	c.mgr.waves = append(c.mgr.waves, opts.Concurrency)
	c.mgr.mu.Unlock()
	return c.NntpClient.StatMany(ctx, ids, opts)
}

// Each Stat attempt runs under its own budget grant, at the granted
// concurrency rather than the requested one. Segments go unproven exactly when
// the pool is saturated, so holding slots across the backoff and a second
// round of Stats would pile load on at the worst moment.
func TestFastFailCheckFilesRetriesUnderSeparateBudgetGrants(t *testing.T) {
	client := fakepool.New()
	client.SetBehavior("seg-1", fakepool.SegmentBehavior{
		Err: fmt.Errorf("nntp: all providers exhausted: %w", nntppool.ErrConnectionDied),
	})

	mgr := &budgetedFastFailManager{
		fastFailPoolManager: fastFailPoolManager{client: client},
		budget:              pool.NewStatBudget(),
	}
	mgr.budget.SetCapacity(4)

	files := []FastFailFile{{Filename: "movie.mkv", Segments: makeTestSegments("seg", 3)}}
	// The sweep ends inconclusive after the bounded retries — that is main's
	// contract and not what this test is about. What matters is how the
	// attempts were admitted.
	_, _ = FastFailCheckFiles(context.Background(), files, mgr, 100, 3, time.Second, nil)

	mgr.mu.Lock()
	events := append([]string(nil), mgr.events...)
	waves := append([]int(nil), mgr.waves...)
	mgr.mu.Unlock()

	for i := 0; i+2 < len(events); i += 3 {
		if events[i] != "acquire" || events[i+1] != "stat" || events[i+2] != "release" {
			t.Fatalf("events = %v, want repeating acquire/stat/release", events)
		}
	}
	if len(events)%3 != 0 || len(events) < 6 {
		t.Fatalf("events = %v, want at least two complete acquire/stat/release cycles", events)
	}
	for i, c := range waves {
		if c > 4 {
			t.Fatalf("wave %d ran at concurrency %d, want <= 4 (the budget grant); waves=%v", i, c, waves)
		}
	}
}
