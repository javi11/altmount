package nzbfilesystem

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// countingRepair is a RepairService whose RepairFile can be made to block, so
// tests can observe deduplication and bounded waits.
type countingRepair struct {
	mu    sync.Mutex
	calls int
	block chan struct{} // when non-nil, RepairFile waits on it (or ctx)
	err   error
}

func (c *countingRepair) RepairFile(ctx context.Context, _ string) error {
	c.mu.Lock()
	c.calls++
	b := c.block
	c.mu.Unlock()
	if b != nil {
		select {
		case <-b:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return c.err
}

func (c *countingRepair) ProbeRepairable(context.Context, string) (bool, bool, error) {
	return false, false, nil
}

func (c *countingRepair) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestRepairManagerDedup verifies concurrent Start calls for one path share a
// single in-flight job (one underlying RepairFile), and a successful job is
// remembered by HealedRecently.
func TestRepairManagerDedup(t *testing.T) {
	cr := &countingRepair{block: make(chan struct{})}
	m := NewRepairManager(cr, nil)

	jobs := make([]*repairJob, 5)
	for i := range jobs {
		jobs[i] = m.Start("/movies/a.mkv")
	}
	for _, j := range jobs {
		if j != jobs[0] {
			t.Fatal("Start did not deduplicate concurrent callers to one job")
		}
	}

	close(cr.block) // let the job finish
	if err := m.Wait(context.Background(), jobs[0]); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := cr.callCount(); got != 1 {
		t.Fatalf("RepairFile called %d times, want 1", got)
	}
	if !m.HealedRecently("/movies/a.mkv") {
		t.Fatal("HealedRecently false after a successful heal")
	}
}

// TestRepairManagerWaitTimeout verifies Wait returns when the caller's deadline
// elapses even though the underlying job is still running.
func TestRepairManagerWaitTimeout(t *testing.T) {
	cr := &countingRepair{block: make(chan struct{})} // never closed by the test path
	m := NewRepairManager(cr, nil)
	j := m.Start("/movies/b.mkv")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := m.Wait(ctx, j); err == nil {
		t.Fatal("Wait returned nil; expected a deadline error while the job blocks")
	}
	if m.HealedRecently("/movies/b.mkv") {
		t.Fatal("HealedRecently true for a job that never completed")
	}
	close(cr.block) // unblock so the background goroutine exits cleanly
}

// TestRepairManagerWaitError verifies a failed repair propagates its error and
// is not remembered as healed.
func TestRepairManagerWaitError(t *testing.T) {
	cr := &countingRepair{err: errors.New("insufficient recovery")}
	m := NewRepairManager(cr, nil)

	if err := m.Wait(context.Background(), m.Start("/movies/c.mkv")); err == nil {
		t.Fatal("Wait returned nil; expected the repair error")
	}
	if m.HealedRecently("/movies/c.mkv") {
		t.Fatal("a failed repair must not be marked healed")
	}
}
