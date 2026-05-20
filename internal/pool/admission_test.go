package pool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubStreamSource is a tiny test source whose ActiveStreams value can be
// changed atomically.
type stubStreamSource struct {
	n atomic.Int64
}

func (s *stubStreamSource) ActiveStreams() int { return int(s.n.Load()) }
func (s *stubStreamSource) set(v int64)        { s.n.Store(v) }

// waitFor polls cond up to d for true; returns false on timeout.
func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

func TestImportAdmission_DisabledIsNoOp(t *testing.T) {
	a := NewImportAdmission()
	// caps (0, 0) -> disabled: every Acquire returns immediately, no queueing.
	for i := range 100 {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		release, err := a.Acquire(ctx)
		cancel()
		if err != nil {
			t.Fatalf("Acquire %d failed: %v", i, err)
		}
		release()
	}

	a.mu.Lock()
	if a.inFlight != 0 {
		t.Fatalf("disabled controller leaked inFlight=%d", a.inFlight)
	}
	a.mu.Unlock()
}

func TestImportAdmission_BlocksAtCap(t *testing.T) {
	a := NewImportAdmission()
	a.SetCaps(2, 1)

	r1, err := a.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	r2, err := a.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire 2: %v", err)
	}

	// Third acquire must block.
	acquired := make(chan struct{})
	go func() {
		r, err := a.Acquire(context.Background())
		if err != nil {
			t.Errorf("acquire 3: %v", err)
			return
		}
		close(acquired)
		r()
	}()

	select {
	case <-acquired:
		t.Fatal("third Acquire should not have returned while cap=2 was full")
	case <-time.After(50 * time.Millisecond):
		// Good — it's blocked.
	}

	// Release one — third should be granted.
	r1()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("third Acquire did not unblock after release")
	}
	r2()
}

func TestImportAdmission_FIFO(t *testing.T) {
	a := NewImportAdmission()
	a.SetCaps(1, 1)

	// Hold the single slot.
	hold, err := a.Acquire(context.Background())
	if err != nil {
		t.Fatalf("hold: %v", err)
	}

	const n = 5
	order := make(chan int, n)
	var wg sync.WaitGroup
	for idx := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := a.Acquire(context.Background())
			if err != nil {
				t.Errorf("waiter %d: %v", idx, err)
				return
			}
			order <- idx
			// Small pause so the next waiter cleanly sees us drop.
			time.Sleep(2 * time.Millisecond)
			r()
		}()
		// Ensure goroutines enqueue in order. The Acquire path takes the lock
		// then registers as a waiter atomically, so a brief sleep is enough to
		// avoid scheduling races for this test.
		time.Sleep(2 * time.Millisecond)
	}

	// Release the held slot — the queue should drain FIFO.
	hold()
	wg.Wait()
	close(order)

	want := 0
	for got := range order {
		if got != want {
			t.Fatalf("FIFO violation: got %d, want %d", got, want)
		}
		want++
	}
}

func TestImportAdmission_StreamAwareCap(t *testing.T) {
	src := &stubStreamSource{}
	a := NewImportAdmission()
	a.SetStreamSource(src)
	a.SetCaps(3, 1)

	// No streams -> capIdle=3.
	r1, _ := a.Acquire(context.Background())
	r2, _ := a.Acquire(context.Background())
	r3, _ := a.Acquire(context.Background())

	// Activate streams. Existing in-flight imports remain.
	src.set(1)
	a.NotifyStreamChange()

	// A fourth must block because capWhileStreaming=1 and inFlight=3.
	blocked := make(chan struct{})
	go func() {
		r, err := a.Acquire(context.Background())
		if err == nil {
			close(blocked)
			r()
		}
	}()
	select {
	case <-blocked:
		t.Fatal("Acquire should be blocked while streams active and inFlight > capWhileStreaming")
	case <-time.After(50 * time.Millisecond):
	}

	// Release two: inFlight=1 == capWhileStreaming, still no admission.
	r1()
	r2()
	select {
	case <-blocked:
		t.Fatal("Acquire should still be blocked at capWhileStreaming")
	case <-time.After(50 * time.Millisecond):
	}

	// Stop streaming — cap returns to 3 and the waiter is granted.
	src.set(0)
	a.NotifyStreamChange()
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("Acquire should have been granted after streams ended")
	}
	r3()
}

func TestImportAdmission_GrowOnSetCapsWakesWaiters(t *testing.T) {
	a := NewImportAdmission()
	a.SetCaps(1, 1)

	hold, _ := a.Acquire(context.Background())

	const n = 3
	granted := make(chan int, n)
	var releases []func()
	var rmu sync.Mutex
	for idx := range n {
		go func() {
			r, err := a.Acquire(context.Background())
			if err != nil {
				t.Errorf("waiter %d: %v", idx, err)
				return
			}
			rmu.Lock()
			releases = append(releases, r)
			rmu.Unlock()
			granted <- idx
		}()
	}

	// Wait for them to enqueue.
	if !waitFor(time.Second, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return len(a.waiters) == n
	}) {
		t.Fatalf("expected %d waiters", n)
	}

	// Grow capIdle to fit them all without releasing the holder.
	a.SetCaps(1+n, 1)

	for i := range n {
		select {
		case <-granted:
		case <-time.After(time.Second):
			t.Fatalf("waiter %d not granted after SetCaps grew the cap", i)
		}
	}

	hold()
	rmu.Lock()
	for _, r := range releases {
		r()
	}
	rmu.Unlock()
}

func TestImportAdmission_CtxCancelRemovesWaiter(t *testing.T) {
	a := NewImportAdmission()
	a.SetCaps(1, 1)

	hold, _ := a.Acquire(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := a.Acquire(ctx)
		done <- err
	}()

	// Wait for the waiter to be enqueued.
	if !waitFor(time.Second, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return len(a.waiters) == 1
	}) {
		t.Fatal("waiter never enqueued")
	}

	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected ctx error on cancel, got nil")
		}
	case <-time.After(time.Second):
		t.Fatal("Acquire did not return after ctx cancellation")
	}

	// Waiter slot must be reclaimed; inFlight must remain 1 (the holder).
	a.mu.Lock()
	if len(a.waiters) != 0 {
		t.Fatalf("expected 0 waiters after cancel, got %d", len(a.waiters))
	}
	if a.inFlight != 1 {
		t.Fatalf("expected inFlight=1, got %d", a.inFlight)
	}
	a.mu.Unlock()

	hold()
}

func TestImportAdmission_CtxCancelRaceForwardsGrant(t *testing.T) {
	// Race condition: a waiter is granted at the same moment its ctx cancels.
	// We must not consume the grant and starve the next waiter.
	a := NewImportAdmission()
	a.SetCaps(1, 1)

	hold, _ := a.Acquire(context.Background())

	ctxA, cancelA := context.WithCancel(context.Background())
	doneA := make(chan error, 1)
	go func() {
		r, err := a.Acquire(ctxA)
		if err == nil {
			// Real callers must release on success even if their ctx fired
			// concurrently; the controller only forwards the grant when the
			// ctx.Done branch is the one selected.
			r()
		}
		doneA <- err
	}()

	// Second waiter (B) is queued behind A.
	doneB := make(chan struct{})
	go func() {
		r, err := a.Acquire(context.Background())
		if err == nil {
			close(doneB)
			r()
		}
	}()

	if !waitFor(time.Second, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return len(a.waiters) == 2
	}) {
		t.Fatal("expected 2 waiters")
	}

	// Race: release & cancel A simultaneously. Whichever fires first, B must
	// eventually be granted (no lost wake-up).
	go cancelA()
	hold()

	select {
	case <-doneA:
	case <-time.After(time.Second):
		t.Fatal("A never returned")
	}
	select {
	case <-doneB:
	case <-time.After(time.Second):
		t.Fatal("B never granted — lost wake-up bug")
	}
}
