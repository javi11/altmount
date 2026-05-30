package pool

import (
	"context"
	"sync"
)

// StreamActivitySource reports how many streams are currently active.
// Implemented by api.StreamTracker; kept here so the dependency flows api -> pool.
type StreamActivitySource interface {
	ActiveStreams() int
}

// ImportAdmission is an adaptive counting semaphore that gates how many NZB
// imports may run concurrently end-to-end. It exposes two caps:
//
//   - capIdle: maximum concurrent imports when no stream is active.
//   - capWhileStreaming: maximum concurrent imports while any stream is active.
//
// A cap value of 0 means "unlimited" for that mode. When both caps are 0 the
// admission controller is a no-op, which is the default so existing
// deployments behave exactly as before until the new config knobs are set.
//
// The controller uses a FIFO waiter queue with manual select / channel signalling
// so we can support ctx cancellation without dropping wake-ups (the classic
// "lost wakeup" hazard with sync.Cond.Wait + cancellation).
type ImportAdmission struct {
	mu                sync.Mutex
	capIdle           int
	capWhileStreaming int
	inFlight          int
	waiters           []*waiter
	streamSource      StreamActivitySource
}

type waiter struct {
	// ch is closed (or sent on) exactly once when the waiter is granted a slot.
	// Buffered with capacity 1 so a granter never blocks; on a race with ctx
	// cancellation, the cancelling goroutine drains and forwards the grant to
	// the next waiter to avoid losing the wake-up.
	ch chan struct{}
}

// NewImportAdmission constructs an admission controller with both caps disabled
// (0/0 = unlimited). Use SetCaps and SetStreamSource to configure it.
func NewImportAdmission() *ImportAdmission {
	return &ImportAdmission{}
}

// SetCaps updates both caps. If capWhileStreaming > capIdle and capIdle > 0,
// the streaming cap is clamped to capIdle. After updating, queued waiters are
// woken if the effective cap grew.
func (a *ImportAdmission) SetCaps(capIdle, capWhileStreaming int) {
	if capIdle < 0 {
		capIdle = 0
	}
	if capWhileStreaming < 0 {
		capWhileStreaming = 0
	}
	if capIdle > 0 && capWhileStreaming > capIdle {
		capWhileStreaming = capIdle
	}

	a.mu.Lock()
	a.capIdle = capIdle
	a.capWhileStreaming = capWhileStreaming
	a.wakeWaitersLocked()
	a.mu.Unlock()
}

// SetStreamSource wires the activity signal. nil sources are tolerated and
// effectively pin behaviour to capIdle.
func (a *ImportAdmission) SetStreamSource(src StreamActivitySource) {
	a.mu.Lock()
	a.streamSource = src
	a.wakeWaitersLocked()
	a.mu.Unlock()
}

// NotifyStreamChange should be called when the stream count changes so the
// controller can wake or hold waiters according to the new effective cap.
func (a *ImportAdmission) NotifyStreamChange() {
	a.mu.Lock()
	a.wakeWaitersLocked()
	a.mu.Unlock()
}

// Acquire blocks until an admission slot is available or ctx is cancelled.
// The returned release function MUST be called exactly once when the import is
// done. When both caps are 0 the call is a fast-path no-op.
func (a *ImportAdmission) Acquire(ctx context.Context) (release func(), err error) {
	a.mu.Lock()
	if a.disabledLocked() {
		a.mu.Unlock()
		return noopRelease, nil
	}

	if a.inFlight < a.currentCapLocked() {
		a.inFlight++
		a.mu.Unlock()
		return a.releaseOnce(), nil
	}

	w := &waiter{ch: make(chan struct{}, 1)}
	a.waiters = append(a.waiters, w)
	a.mu.Unlock()

	select {
	case <-w.ch:
		// Granted. inFlight was already incremented by the granter.
		return a.releaseOnce(), nil
	case <-ctx.Done():
		// We may have been granted concurrently. Resolve the race under the
		// lock: if the channel has a pending wake, consume it and forward it
		// to the next waiter; otherwise remove ourselves from the queue.
		a.mu.Lock()
		select {
		case <-w.ch:
			// Already granted. Hand the slot to the next waiter.
			a.inFlight-- // undo the grant
			a.wakeWaitersLocked()
		default:
			a.removeWaiterLocked(w)
		}
		a.mu.Unlock()
		return noopRelease, ctx.Err()
	}
}

// disabledLocked reports true when both caps are 0 (controller is a no-op).
func (a *ImportAdmission) disabledLocked() bool {
	return a.capIdle == 0 && a.capWhileStreaming == 0
}

// currentCapLocked returns the cap that applies right now. A cap of 0 means
// unlimited; we return a very large number so the comparison `inFlight < cap`
// is effectively always true.
func (a *ImportAdmission) currentCapLocked() int {
	cap := a.capIdle
	if a.streamSource != nil && a.streamSource.ActiveStreams() > 0 {
		cap = a.capWhileStreaming
	}
	if cap <= 0 {
		// Unlimited.
		return 1 << 30
	}
	return cap
}

// wakeWaitersLocked wakes waiters in FIFO order while there is headroom under
// the current cap. Each wake-up increments inFlight, so callers that receive
// the signal must call their release exactly once.
func (a *ImportAdmission) wakeWaitersLocked() {
	if a.disabledLocked() {
		// Drain any waiters (shouldn't exist when both caps are 0, but be safe).
		for _, w := range a.waiters {
			select {
			case w.ch <- struct{}{}:
				a.inFlight++
			default:
			}
		}
		a.waiters = nil
		return
	}

	cap := a.currentCapLocked()
	for len(a.waiters) > 0 && a.inFlight < cap {
		w := a.waiters[0]
		a.waiters = a.waiters[1:]
		a.inFlight++
		// Buffered chan capacity 1 — never blocks.
		w.ch <- struct{}{}
	}
}

func (a *ImportAdmission) removeWaiterLocked(target *waiter) {
	for i, w := range a.waiters {
		if w == target {
			a.waiters = append(a.waiters[:i], a.waiters[i+1:]...)
			return
		}
	}
}

func (a *ImportAdmission) releaseOnce() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			if a.inFlight > 0 {
				a.inFlight--
			}
			a.wakeWaitersLocked()
			a.mu.Unlock()
		})
	}
}

func noopRelease() {}
