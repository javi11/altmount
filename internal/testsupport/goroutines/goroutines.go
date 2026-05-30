// Package goroutines provides a small helper for tests that need to assert
// goroutine leaks — specifically, the closeWg accumulation scenarios in the
// streaming pipeline where rapid seek/close cycles can spawn background
// goroutines that outlive the request.
//
// Goroutine counting is inherently noisy: the Go runtime spins up internal
// goroutines (GC, sysmon, http2 pingers, finalizer goroutines) that come
// and go independently of test code. The Snapshot / AssertReturnedToBaseline
// pattern below tolerates that noise with a small slack window and a polling
// timeout, while still catching the "N spawned, none reaped" leaks this
// codebase has historically been vulnerable to.
//
// Usage:
//
//	snap := goroutines.Take(t)
//	doWorkThatMustNotLeak()
//	snap.AssertReturnedToBaseline(t, 5*time.Second)
//
// The assertion polls runtime.NumGoroutine() until it lands within
// DefaultSlack of the original count or the timeout fires.
package goroutines

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

// DefaultSlack is the tolerance allowed when comparing goroutine counts
// before and after a test phase. Set high enough to absorb runtime jitter
// (GC workers, finalizers) but low enough that a real leak of >5 goroutines
// is caught.
const DefaultSlack = 5

// Snapshot is a point-in-time goroutine count.
type Snapshot struct {
	baseline int
}

// Take records the current goroutine count. Call once before the code under
// test, then call AssertReturnedToBaseline after it should have settled.
func Take(t testing.TB) Snapshot {
	t.Helper()
	// Two GCs and a short pause give the runtime a chance to reap goroutines
	// that finished but haven't been removed from runtime.NumGoroutine yet.
	// This keeps the baseline tight without making the helper itself a
	// source of flakes.
	runtime.GC()
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	return Snapshot{baseline: runtime.NumGoroutine()}
}

// Baseline returns the recorded count.
func (s Snapshot) Baseline() int { return s.baseline }

// AssertReturnedToBaseline polls runtime.NumGoroutine until it reaches
// baseline + DefaultSlack or until timeout elapses. On timeout the test
// fails with a diagnostic that includes the current and target counts and
// a partial dump of the goroutine stacks — usually enough to locate the
// leak source without rerunning under a profiler.
func (s Snapshot) AssertReturnedToBaseline(t testing.TB, timeout time.Duration) {
	s.AssertReturnedToBaselineWithSlack(t, timeout, DefaultSlack)
}

// AssertReturnedToBaselineWithSlack is the explicit-tolerance variant. Use
// when the code under test is known to spawn a fixed number of background
// goroutines that are part of its design (e.g. a worker pool).
func (s Snapshot) AssertReturnedToBaselineWithSlack(t testing.TB, timeout time.Duration, slack int) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	target := s.baseline + slack
	for {
		runtime.Gosched()
		cur := runtime.NumGoroutine()
		if cur <= target {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf(
				"goroutine leak: count=%d, baseline=%d, allowed=%d, timeout=%s\n%s",
				cur, s.baseline, target, timeout, partialStackDump(),
			)
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// partialStackDump returns the first ~8KB of runtime.Stack(_, true) — enough
// to identify the leaking goroutine family without dumping megabytes of
// stack data into the test log.
func partialStackDump() string {
	buf := make([]byte, 8*1024)
	n := runtime.Stack(buf, true)
	return fmt.Sprintf("--- partial goroutine dump (%d bytes truncated to %d) ---\n%s",
		n, len(buf), buf[:n])
}
