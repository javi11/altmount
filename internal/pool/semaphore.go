package pool

import (
	"context"
	"sync"
)

// adaptiveSemaphore is a FIFO counting semaphore whose capacity is computed
// on demand by capLocked, so it can change between acquisitions (config
// updates, stream activity). A computed cap of 0 means "disabled": Acquire is
// a fast-path no-op and any queued waiters are drained.
//
// It uses a FIFO waiter queue with manual select / channel signalling so we
// can support ctx cancellation without dropping wake-ups (the classic
// "lost wakeup" hazard with sync.Cond.Wait + cancellation).
type adaptiveSemaphore struct {
	mu       sync.Mutex
	inFlight int
	waiters  []*waiter
	// capLocked returns the current effective capacity. Called with mu held.
	// 0 means disabled (unlimited, no accounting).
	capLocked func() int
}

type waiter struct {
	// ch is closed (or sent on) exactly once when the waiter is granted a slot.
	// Buffered with capacity 1 so a granter never blocks; on a race with ctx
	// cancellation, the cancelling goroutine drains and forwards the grant to
	// the next waiter to avoid losing the wake-up.
	ch chan struct{}
}

// Acquire blocks until a slot is available or ctx is cancelled. The returned
// release function MUST be called exactly once when the work is done. When
// the current cap is 0 the call is a fast-path no-op.
func (s *adaptiveSemaphore) Acquire(ctx context.Context) (release func(), err error) {
	s.mu.Lock()
	cap := s.capLocked()
	if cap == 0 {
		s.mu.Unlock()
		return noopRelease, nil
	}

	if s.inFlight < cap {
		s.inFlight++
		s.mu.Unlock()
		return s.releaseOnce(), nil
	}

	w := &waiter{ch: make(chan struct{}, 1)}
	s.waiters = append(s.waiters, w)
	s.mu.Unlock()

	select {
	case <-w.ch:
		// Granted. inFlight was already incremented by the granter.
		return s.releaseOnce(), nil
	case <-ctx.Done():
		// We may have been granted concurrently. Resolve the race under the
		// lock: if the channel has a pending wake, consume it and forward it
		// to the next waiter; otherwise remove ourselves from the queue.
		s.mu.Lock()
		select {
		case <-w.ch:
			// Already granted. Hand the slot to the next waiter.
			s.inFlight-- // undo the grant
			s.wakeWaitersLocked()
		default:
			s.removeWaiterLocked(w)
		}
		s.mu.Unlock()
		return noopRelease, ctx.Err()
	}
}

// wakeWaitersLocked wakes waiters in FIFO order while there is headroom under
// the current cap. Each wake-up increments inFlight, so callers that receive
// the signal must call their release exactly once.
func (s *adaptiveSemaphore) wakeWaitersLocked() {
	cap := s.capLocked()
	if cap == 0 {
		// Disabled — drain any waiters as free grants; their releases are
		// no-ops on the accounting side because inFlight is decremented on
		// release and re-clamped at 0.
		for _, w := range s.waiters {
			select {
			case w.ch <- struct{}{}:
				s.inFlight++
			default:
			}
		}
		s.waiters = nil
		return
	}

	for len(s.waiters) > 0 && s.inFlight < cap {
		w := s.waiters[0]
		s.waiters = s.waiters[1:]
		s.inFlight++
		// Buffered chan capacity 1 — never blocks.
		w.ch <- struct{}{}
	}
}

func (s *adaptiveSemaphore) removeWaiterLocked(target *waiter) {
	for i, w := range s.waiters {
		if w == target {
			s.waiters = append(s.waiters[:i], s.waiters[i+1:]...)
			return
		}
	}
}

func (s *adaptiveSemaphore) releaseOnce() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			if s.inFlight > 0 {
				s.inFlight--
			}
			s.wakeWaitersLocked()
			s.mu.Unlock()
		})
	}
}

func noopRelease() {}
