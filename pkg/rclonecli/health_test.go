package rclonecli

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

// newHealthTestManager builds a minimal Manager wired only with the fields
// performMountHealthCheck touches, plus injectable probe/restart seams so the
// restart decision can be asserted without a live rcd subprocess.
func newHealthTestManager(t *testing.T, probeOK bool, readyAt time.Time) (*Manager, *int32) {
	t.Helper()

	ready := make(chan struct{})
	close(ready) // IsReady() == true

	var restartCalls int32
	m := &Manager{
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		ctx:         context.Background(),
		mounts:      make(map[string]*MountInfo),
		serverReady: ready,
		readyAt:     readyAt,
		probe: func(context.Context, time.Duration) bool {
			return probeOK
		},
	}
	m.restart = func(context.Context) error {
		atomic.AddInt32(&restartCalls, 1)
		return nil
	}
	return m, &restartCalls
}

// afterGrace returns a readyAt timestamp old enough that the startup grace
// period has elapsed.
func afterGrace() time.Time {
	return time.Now().Add(-2 * startupGracePeriod)
}

func TestPerformMountHealthCheck_SuccessResetsFailureStreak(t *testing.T) {
	m, restarts := newHealthTestManager(t, true, afterGrace())
	m.consecutiveProbeFailures = 2

	m.performMountHealthCheck()

	if got := atomic.LoadInt32(restarts); got != 0 {
		t.Fatalf("healthy probe must not restart rcd, got %d restarts", got)
	}
	if m.consecutiveProbeFailures != 0 {
		t.Fatalf("healthy probe must reset failure streak, got %d", m.consecutiveProbeFailures)
	}
}

func TestPerformMountHealthCheck_WithinGraceNeverRestarts(t *testing.T) {
	// readyAt = now -> firmly inside the startup grace period.
	m, restarts := newHealthTestManager(t, false, time.Now())

	// Even well past the failure threshold, no restart may happen during grace.
	for range maxConsecutiveProbeFailures + 2 {
		m.performMountHealthCheck()
	}

	if got := atomic.LoadInt32(restarts); got != 0 {
		t.Fatalf("must not restart rcd during startup grace period, got %d restarts", got)
	}
}

func TestPerformMountHealthCheck_BelowThresholdDoesNotRestart(t *testing.T) {
	m, restarts := newHealthTestManager(t, false, afterGrace())

	for range maxConsecutiveProbeFailures - 1 {
		m.performMountHealthCheck()
	}

	if got := atomic.LoadInt32(restarts); got != 0 {
		t.Fatalf("must not restart below threshold, got %d restarts", got)
	}
	if m.consecutiveProbeFailures != maxConsecutiveProbeFailures-1 {
		t.Fatalf("failure streak = %d, want %d", m.consecutiveProbeFailures, maxConsecutiveProbeFailures-1)
	}
}

func TestPerformMountHealthCheck_AtThresholdRestartsOnceAndResets(t *testing.T) {
	m, restarts := newHealthTestManager(t, false, afterGrace())

	for range maxConsecutiveProbeFailures {
		m.performMountHealthCheck()
	}

	if got := atomic.LoadInt32(restarts); got != 1 {
		t.Fatalf("expected exactly 1 restart at threshold, got %d", got)
	}
	if m.consecutiveProbeFailures != 0 {
		t.Fatalf("failure streak must reset after restart, got %d", m.consecutiveProbeFailures)
	}
}

func TestPerformMountHealthCheck_NotReadyIsNoOp(t *testing.T) {
	m, restarts := newHealthTestManager(t, false, afterGrace())
	m.serverReady = make(chan struct{}) // open -> IsReady() == false

	m.performMountHealthCheck()

	if got := atomic.LoadInt32(restarts); got != 0 {
		t.Fatalf("must not restart before server is ready, got %d restarts", got)
	}
	if m.consecutiveProbeFailures != 0 {
		t.Fatalf("must not touch failure streak before ready, got %d", m.consecutiveProbeFailures)
	}
}
