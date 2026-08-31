package pool

import (
	"context"
)

// StreamActivitySource reports how many streams are currently active.
// Implemented by api.StreamTracker; kept here so the dependency flows api -> pool.
type StreamActivitySource interface {
	ActiveStreams() int
}

// ImportAdmission is a counting semaphore that gates how many NZB imports may
// run concurrently end-to-end. A cap of 0 means "unlimited" (the controller is
// a no-op), which is the default so deployments behave as before until the
// max_concurrent_imports config knob is set.
//
// Connection-level balancing between imports and streams is handled separately
// by ImportBudget; this gate only bounds whole-import parallelism (CPU, disk,
// queue pressure).
type ImportAdmission struct {
	sem adaptiveSemaphore
	cap int
}

// NewImportAdmission constructs an admission controller with the cap disabled
// (0 = unlimited). Use SetCap to configure it.
func NewImportAdmission() *ImportAdmission {
	a := &ImportAdmission{}
	a.sem.capLocked = func() int { return a.cap }
	return a
}

// SetCap updates the cap. Queued waiters are woken if the cap grew.
// A cap of 0 disables the gate (unlimited).
func (a *ImportAdmission) SetCap(cap int) {
	if cap < 0 {
		cap = 0
	}
	a.sem.mu.Lock()
	a.cap = cap
	a.sem.wakeWaitersLocked()
	a.sem.mu.Unlock()
}

// Acquire blocks until an admission slot is available or ctx is cancelled.
// The returned release function MUST be called exactly once when the import is
// done. When the cap is 0 the call is a fast-path no-op.
func (a *ImportAdmission) Acquire(ctx context.Context) (release func(), err error) {
	return a.sem.Acquire(ctx)
}
