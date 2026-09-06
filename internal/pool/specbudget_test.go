package pool

import "testing"

func TestSpeculativeBudgetCapacityFormula(t *testing.T) {
	b := NewSpeculativeBudget()
	b.SetCapacity(50)
	if got := b.Capacity(); got != 199 {
		t.Fatalf("Capacity(50 conns) = %d, want 199", got)
	}
	b.SetCapacity(15)
	if got := b.Capacity(); got != 59 {
		t.Fatalf("Capacity(15 conns) = %d, want 59", got)
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
	b.SetCapacity(1) // 1*4-1 = 3 slots
	r1, ok1 := b.TryAcquire()
	r2, ok2 := b.TryAcquire()
	r3, ok3 := b.TryAcquire()
	if !ok1 || !ok2 || !ok3 {
		t.Fatal("three slots expected")
	}
	if _, ok := b.TryAcquire(); ok {
		t.Fatal("fourth acquire must be refused")
	}
	r3()
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
