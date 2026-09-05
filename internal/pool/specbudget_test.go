package pool

import "testing"

func TestSpeculativeBudgetCapacityFormula(t *testing.T) {
	b := NewSpeculativeBudget()
	b.SetCapacity(50)
	if got := b.Capacity(); got != 149 {
		t.Fatalf("Capacity(50 conns) = %d, want 149", got)
	}
	b.SetCapacity(0)
	if got := b.Capacity(); got != 0 {
		t.Fatalf("Capacity(0) = %d, want 0 (unlimited)", got)
	}
	for i := 0; i < 1000; i++ {
		if _, ok := b.TryAcquire(); !ok {
			t.Fatal("unlimited budget must always grant")
		}
	}
	if b.InFlight() != 0 {
		t.Fatal("unlimited budget must not account")
	}
}

func TestSpeculativeBudgetBoundsAndReleases(t *testing.T) {
	b := NewSpeculativeBudget()
	b.SetCapacity(1) // 1*3-1 = 2 slots
	r1, ok1 := b.TryAcquire()
	r2, ok2 := b.TryAcquire()
	if !ok1 || !ok2 {
		t.Fatal("two slots expected")
	}
	if _, ok := b.TryAcquire(); ok {
		t.Fatal("third acquire must be refused")
	}
	r1()
	r1() // idempotent
	if b.InFlight() != 1 {
		t.Fatalf("InFlight after one release = %d, want 1", b.InFlight())
	}
	if _, ok := b.TryAcquire(); !ok {
		t.Fatal("slot must be reusable after release")
	}
	r2()
}

func TestSpeculativeBudgetNilIsUnlimited(t *testing.T) {
	var b *SpeculativeBudget
	release, ok := b.TryAcquire()
	if !ok {
		t.Fatal("nil budget must grant")
	}
	release()
	b.SetCapacity(10)
}
