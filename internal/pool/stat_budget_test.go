package pool

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStatBudgetGrantsUpToFreeCapacity(t *testing.T) {
	b := NewStatBudget()
	b.SetCapacity(10)

	got, release1, err := b.Acquire(context.Background(), 6)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got != 6 {
		t.Fatalf("first grant = %d, want 6", got)
	}

	got, release2, err := b.Acquire(context.Background(), 6)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got != 4 {
		t.Fatalf("second grant = %d, want 4 (remaining capacity)", got)
	}
	if inFlight := b.InFlight(); inFlight != 10 {
		t.Fatalf("InFlight = %d, want 10", inFlight)
	}

	release1()
	release2()
	if inFlight := b.InFlight(); inFlight != 0 {
		t.Fatalf("InFlight after release = %d, want 0", inFlight)
	}
}

func TestStatBudgetDisabledGrantsFullRequest(t *testing.T) {
	b := NewStatBudget()

	got, release, err := b.Acquire(context.Background(), 42)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got != 42 {
		t.Fatalf("grant = %d, want 42 when the budget is disabled", got)
	}
	release()
}

func TestStatBudgetBlocksUntilRelease(t *testing.T) {
	b := NewStatBudget()
	b.SetCapacity(4)

	_, release, err := b.Acquire(context.Background(), 4)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	granted := make(chan int, 1)
	go func() {
		g, rel, err := b.Acquire(context.Background(), 4)
		if err != nil {
			granted <- -1
			return
		}
		defer rel()
		granted <- g
	}()

	select {
	case g := <-granted:
		t.Fatalf("second Acquire returned %d while the budget was fully held", g)
	case <-time.After(50 * time.Millisecond):
	}

	release()

	select {
	case g := <-granted:
		if g != 4 {
			t.Fatalf("grant after release = %d, want 4", g)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second Acquire never completed after the slots were released")
	}
}

func TestStatBudgetQueuedCallerIsServedBeforeNewcomer(t *testing.T) {
	b := NewStatBudget()
	b.SetCapacity(2)

	_, release, err := b.Acquire(context.Background(), 2)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	order := make(chan string, 2)
	queued := make(chan struct{})
	go func() {
		close(queued)
		_, rel, err := b.Acquire(context.Background(), 2)
		if err == nil {
			order <- "queued"
			rel()
		}
	}()
	<-queued
	// Give the queued caller time to actually enqueue before the newcomer runs.
	time.Sleep(50 * time.Millisecond)

	go func() {
		_, rel, err := b.Acquire(context.Background(), 2)
		if err == nil {
			order <- "newcomer"
			rel()
		}
	}()
	time.Sleep(50 * time.Millisecond)

	release()

	select {
	case first := <-order:
		if first != "queued" {
			t.Fatalf("first grant went to %q, want the already-queued caller", first)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no caller was granted after release")
	}
}

func TestStatBudgetAcquireHonoursContextCancellation(t *testing.T) {
	b := NewStatBudget()
	b.SetCapacity(1)

	_, release, err := b.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	got, rel, err := b.Acquire(ctx, 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if got != 0 {
		t.Fatalf("grant = %d, want 0 on cancellation", got)
	}
	rel()

	if inFlight := b.InFlight(); inFlight != 1 {
		t.Fatalf("InFlight = %d, want 1 — a cancelled waiter must not leak slots", inFlight)
	}
}

func TestStatBudgetSetCapacityWakesWaiters(t *testing.T) {
	b := NewStatBudget()
	b.SetCapacity(1)

	_, release, err := b.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	granted := make(chan int, 1)
	go func() {
		g, rel, err := b.Acquire(context.Background(), 3)
		if err != nil {
			granted <- -1
			return
		}
		defer rel()
		granted <- g
	}()

	time.Sleep(50 * time.Millisecond)
	b.SetCapacity(4)

	select {
	case g := <-granted:
		if g != 3 {
			t.Fatalf("grant after capacity growth = %d, want 3", g)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter was not woken when the capacity grew")
	}
}

// The budget's capacity is StatConcurrency — the pool's STAT pipeline depth —
// while a health sweep asks for the providers' connection count. Keeping the
// two quantities distinct is what lets health and import verification run at
// the same time instead of taking turns: health takes its connection-sized
// slice and the rest of the pipeline depth stays available.
func TestManagerStatBudgetSharesCapacityBetweenHealthAndImport(t *testing.T) {
	ctx := context.Background()
	m := NewManager(ctx, nil)
	// What RegisterConfigHandlers wires from Config.StatConcurrency().
	m.SetStatCapacity(200)

	// What a health sweep asks for: Config.GetMaxConnectionsForHealthChecks().
	health, releaseHealth, err := m.AcquireStatSlots(ctx, 50)
	if err != nil {
		t.Fatalf("health acquire: %v", err)
	}
	defer releaseHealth()
	if health != 50 {
		t.Fatalf("health granted %d, want 50", health)
	}

	imp, releaseImport, err := m.AcquireStatSlots(ctx, 500)
	if err != nil {
		t.Fatalf("import acquire: %v", err)
	}
	defer releaseImport()

	if imp != 150 {
		t.Fatalf("import granted %d, want 150 — health must not swallow the whole budget", imp)
	}
}

// Disabling the budget drains queued waiters as free grants without charging
// them against inFlight. Their release must therefore be a no-op: if it
// decrements, re-enabling the budget leaves it believing slots are free that
// a live holder still owns, and it over-admits.
func TestStatBudgetDisabledDrainDoesNotCorruptAccounting(t *testing.T) {
	b := NewStatBudget()
	b.SetCapacity(4)

	_, releaseHolder, err := b.Acquire(context.Background(), 4)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer releaseHolder()

	drained := make(chan func(), 1)
	go func() {
		_, rel, err := b.Acquire(context.Background(), 4)
		if err != nil {
			return
		}
		drained <- rel
	}()
	time.Sleep(50 * time.Millisecond)

	b.SetCapacity(0)

	var releaseDrained func()
	select {
	case releaseDrained = <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("disabling the budget did not drain the queued waiter")
	}

	b.SetCapacity(4)
	releaseDrained()

	if inFlight := b.InFlight(); inFlight != 4 {
		t.Fatalf("InFlight = %d, want 4 — the holder still owns every slot; an "+
			"unaccounted grant must not release slots it never took", inFlight)
	}
}
