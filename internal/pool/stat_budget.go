package pool

import (
	"context"
	"sync"
)

// StatBudget bounds the total number of outstanding STAT (article existence)
// checks pool-wide, across every caller: import release probes, per-file
// import validation, scheduled health sweeps and manual health checks. They
// all share a single NNTP pool, so without a shared cap their independent
// concurrency limits simply add up; the excess queues inside the pool where
// waiting requests burn their own deadlines and time out as if the articles
// were missing.
//
// Callers ask for the concurrency they would like and are granted whatever
// the budget can spare (never less than 1 once admitted). Grants are handed
// out in FIFO order, so a long-running sweep cannot starve a newcomer that is
// already queued.
//
// A capacity of 0 disables the budget: Acquire grants the full request
// immediately without accounting, and the grant releases as a no-op. That
// keeps pool-less paths and test fakes deadlock-free.
type StatBudget struct {
	mu       sync.Mutex
	capacity int
	inFlight int
	waiters  []*statWaiter
}

type statWaiter struct {
	want int
	// granted is written by the granter under mu before ch is signalled.
	granted int
	// accounted is false when the grant was handed out without charging
	// inFlight — the disabled-budget path. Such a grant must release as a
	// no-op, or it returns slots it never took.
	accounted bool
	// ch is buffered with capacity 1 so a granter never blocks; on a race with
	// ctx cancellation the cancelling goroutine drains it and returns the
	// slots so the wake-up is not lost.
	ch chan struct{}
}

// NewStatBudget constructs a budget with capacity 0 (disabled). Use
// SetCapacity to configure it.
func NewStatBudget() *StatBudget {
	return &StatBudget{}
}

// SetCapacity updates the total number of concurrent STAT checks allowed
// across all callers. Values below 0 are clamped to 0 (disabled). Queued
// waiters are woken if the new capacity leaves headroom.
func (b *StatBudget) SetCapacity(n int) {
	if n < 0 {
		n = 0
	}
	b.mu.Lock()
	b.capacity = n
	b.wakeLocked()
	b.mu.Unlock()
}

// Capacity returns the configured capacity.
func (b *StatBudget) Capacity() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.capacity
}

// InFlight returns how many STAT slots are currently held. Exposed for
// metrics and tests.
func (b *StatBudget) InFlight() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inFlight
}

// Acquire blocks until the budget can grant at least one slot or ctx is
// cancelled. It returns the granted concurrency (1..want) and a release
// function that MUST be called exactly once when the sweep wave is done.
func (b *StatBudget) Acquire(ctx context.Context, want int) (granted int, release func(), err error) {
	if want < 1 {
		want = 1
	}

	b.mu.Lock()
	if b.capacity <= 0 {
		b.mu.Unlock()
		return want, noopRelease, nil
	}
	// Only jump the queue when nobody is already waiting, so grants stay FIFO
	// and a queued caller is always served next.
	if len(b.waiters) == 0 {
		if free := b.capacity - b.inFlight; free > 0 {
			g := min(want, free)
			b.inFlight += g
			b.mu.Unlock()
			return g, b.releaseOnce(g), nil
		}
	}
	w := &statWaiter{want: want, ch: make(chan struct{}, 1)}
	b.waiters = append(b.waiters, w)
	b.mu.Unlock()

	select {
	case <-w.ch:
		if !w.accounted {
			return w.granted, noopRelease, nil
		}
		return w.granted, b.releaseOnce(w.granted), nil
	case <-ctx.Done():
		b.mu.Lock()
		select {
		case <-w.ch:
			// Granted concurrently with the cancellation — hand the slots back
			// so they are not leaked to a caller that will never use them.
			if w.accounted {
				b.releaseLocked(w.granted)
			}
		default:
			b.removeWaiterLocked(w)
		}
		b.mu.Unlock()
		return 0, noopRelease, ctx.Err()
	}
}

// wakeLocked grants slots to queued waiters in FIFO order. Called with mu held.
func (b *StatBudget) wakeLocked() {
	if b.capacity <= 0 {
		// Disabled — drain waiters as unaccounted free grants.
		for _, w := range b.waiters {
			w.granted = w.want
			w.accounted = false
			w.ch <- struct{}{}
		}
		b.waiters = nil
		return
	}
	for len(b.waiters) > 0 {
		free := b.capacity - b.inFlight
		if free <= 0 {
			return
		}
		w := b.waiters[0]
		b.waiters = b.waiters[1:]
		g := min(w.want, free)
		b.inFlight += g
		w.granted = g
		w.accounted = true
		w.ch <- struct{}{}
	}
}

func (b *StatBudget) removeWaiterLocked(target *statWaiter) {
	for i, w := range b.waiters {
		if w == target {
			b.waiters = append(b.waiters[:i], b.waiters[i+1:]...)
			return
		}
	}
}

func (b *StatBudget) releaseLocked(n int) {
	b.inFlight -= n
	if b.inFlight < 0 {
		b.inFlight = 0
	}
	b.wakeLocked()
}

func (b *StatBudget) releaseOnce(n int) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			b.releaseLocked(n)
			b.mu.Unlock()
		})
	}
}

// StatAdmitter is the shared-budget surface a STAT sweep needs. Implemented by
// Manager.
type StatAdmitter interface {
	// AcquireStatSlots blocks until the shared STAT budget can grant at least
	// one of the requested slots, returning the granted concurrency and a
	// release function that must be called exactly once.
	AcquireStatSlots(ctx context.Context, want int) (granted int, release func(), err error)
}
