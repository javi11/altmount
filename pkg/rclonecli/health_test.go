package rclonecli

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
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
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		ctx:           context.Background(),
		mounts:        make(map[string]*MountInfo),
		serverReady:   ready,
		serverStarted: true,
		readyAt:       readyAt,
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
	m.mu.Lock()
	m.serverReady = make(chan struct{}) // open -> IsReady() == false
	m.mu.Unlock()

	m.performMountHealthCheck()

	if got := atomic.LoadInt32(restarts); got != 0 {
		t.Fatalf("must not restart before server is ready, got %d restarts", got)
	}
	if m.consecutiveProbeFailures != 0 {
		t.Fatalf("must not touch failure streak before ready, got %d", m.consecutiveProbeFailures)
	}
}

func TestPerformMountHealthCheck_LeavesFailedMountMarkedMountedForRecovery(t *testing.T) {
	m, _ := newHealthTestManager(t, true, afterGrace())
	m.mounts["altmount"] = &MountInfo{Provider: "altmount", Mounted: true}
	m.forceUnmount = func(string) error { return nil }
	m.restart = func(context.Context) error { return errors.New("stop recovery") }
	m.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("health check failed")
	})}

	m.performMountHealthCheck()

	info, ok := m.GetMountInfo("altmount")
	if !ok {
		t.Fatal("mount info missing")
	}
	if !info.Mounted {
		t.Fatal("failed mount must remain marked mounted until RecoverMount can unmount and reclaim its VFS")
	}
	if info.Error != "Health check failed" {
		t.Fatalf("unexpected mount error: %q", info.Error)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// withRcdRestartAfter wires a config into the test manager so the derived
// threshold can be asserted.
func withRcdRestartAfter(t *testing.T, m *Manager, value string) {
	t.Helper()

	cfg := &config.Config{}
	cfg.RClone.RcdRestartAfter = value
	m.cfg = config.NewManager(cfg, "")
}

func TestRestartAfterProbeFailures_DerivesCountFromDuration(t *testing.T) {
	for _, tc := range []struct {
		configured string
		want       int
		why        string
	}{
		{"90s", 3, "the default, and the previous hard-coded behaviour"},
		{"30s", 1, "exactly one interval"},
		{"5m", 10, "a tolerant install riding out a long stall"},
		{"45s", 2, "rounds up rather than truncating to one interval"},
		{"1s", 1, "shorter than an interval still means one sustained failure"},
		{"", 3, "unset falls back to the built-in default"},
		{"nonsense", 3, "unparseable falls back rather than disabling the guard"},
		{"-30s", 3, "negative falls back rather than restarting every tick"},
		// Near time.Duration's maximum. The obvious (x+interval-1)/interval form
		// overflows here and collapses to 1, turning the longest tolerance
		// expressible into a restart on every failed probe.
		{"2562047h47m16.854775807s", 307445735, "an absurd but valid duration must not invert into no tolerance"},
	} {
		m, _ := newHealthTestManager(t, false, time.Time{})
		withRcdRestartAfter(t, m, tc.configured)

		if got := m.restartAfterProbeFailures(); got != tc.want {
			t.Errorf("rcd_restart_after=%q gave threshold %d, want %d (%s)",
				tc.configured, got, tc.want, tc.why)
		}
	}
}

func TestRestartAfterProbeFailures_NoConfigUsesDefault(t *testing.T) {
	m, _ := newHealthTestManager(t, false, time.Time{})
	if got := m.restartAfterProbeFailures(); got != maxConsecutiveProbeFailures {
		t.Errorf("with no config wired, threshold = %d, want %d", got, maxConsecutiveProbeFailures)
	}
}

// TestPerformMountHealthCheck_HonoursConfiguredTolerance is the point of the
// change: an install that configures more tolerance rides out a stall that the
// default would have restarted the rcd for, tearing the mount out from under
// every reader.
func TestPerformMountHealthCheck_HonoursConfiguredTolerance(t *testing.T) {
	m, restarts := newHealthTestManager(t, false, time.Now().Add(-time.Hour))
	withRcdRestartAfter(t, m, "5m") // 10 probes

	for range maxConsecutiveProbeFailures + 2 {
		m.performMountHealthCheck()
	}
	if got := atomic.LoadInt32(restarts); got != 0 {
		t.Fatalf("restarted %d times past the default threshold; the configured tolerance was ignored", got)
	}

	for range 5 {
		m.performMountHealthCheck()
	}
	if got := atomic.LoadInt32(restarts); got != 1 {
		t.Fatalf("restarts = %d after reaching the configured threshold, want 1", got)
	}
}
