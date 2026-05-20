package goroutines

import (
	"sync"
	"testing"
	"time"
)

// TestSnapshot_ReturnsToBaselineWhenNoLeak proves the happy path: spawn N
// goroutines, wait for them to finish, and assert the snapshot succeeds.
// Without this the leak-detection tests can't trust a passing run means
// "no leak" — it could just mean the helper is broken.
func TestSnapshot_ReturnsToBaselineWhenNoLeak(t *testing.T) {
	t.Parallel()
	snap := Take(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(5 * time.Millisecond)
		}()
	}
	wg.Wait()

	snap.AssertReturnedToBaseline(t, 2*time.Second)
}

// TestSnapshot_DetectsLeak proves the failure path: a leaked goroutine
// must cause the assertion to fail. We use a sub-test with a custom
// testing.TB shim so we can observe the failure without failing the
// parent test.
func TestSnapshot_DetectsLeak(t *testing.T) {
	t.Parallel()
	snap := Take(t)

	// Spawn a leak intentionally — these will never exit during the test.
	stop := make(chan struct{})
	defer close(stop)
	for i := 0; i < 50; i++ {
		go func() {
			<-stop
		}()
	}

	shim := &recordingTB{realTB: t}
	snap.AssertReturnedToBaselineWithSlack(shim, 300*time.Millisecond, DefaultSlack)
	if !shim.failed {
		t.Errorf("expected leak detection to call Errorf, but it did not")
	}
}

// recordingTB captures Errorf calls so we can assert the helper detected
// the leak without polluting the parent test's pass/fail state.
type recordingTB struct {
	testing.TB
	realTB testing.TB
	failed bool
}

func (r *recordingTB) Helper()                                {}
func (r *recordingTB) Errorf(format string, args ...any)      { r.failed = true }
func (r *recordingTB) Fatalf(format string, args ...any)      { r.failed = true; r.realTB.FailNow() }
func (r *recordingTB) Logf(format string, args ...any)        { r.realTB.Logf(format, args...) }
