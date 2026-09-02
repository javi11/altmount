package pool

import (
	"testing"
	"time"
)

// stepAt drives one controller tick and returns the reserve it settled on.
func stepAt(c *headroomController, sec int, bytes int64, streams int) int {
	c.step(time.Unix(int64(sec), 0), bytes, streams)
	return c.reserve
}

func newTestController(t *testing.T, capacity, costPct int) (*headroomController, *ImportBudget) {
	t.Helper()
	b := NewImportBudget()
	b.SetCapacity(capacity)
	return newHeadroomController(b, HeadroomPolicy{MaxTotalCostPct: costPct}), b
}

func TestHeadroom_NoStreamsReleasesReserve(t *testing.T) {
	c, b := newTestController(t, 100, 0)

	// Climb a little with a stream active...
	stepAt(c, 0, 0, 1)
	stepAt(c, 10, 4_000_000_000, 1)
	if got := stepAt(c, 20, 8_000_000_000, 1); got == 0 {
		t.Fatal("reserve should have climbed while a stream was active")
	}

	// ...then the stream ends: import must get the whole pool back.
	if got := stepAt(c, 30, 12_000_000_000, 0); got != 0 {
		t.Errorf("reserve = %d with no streams, want 0", got)
	}
	if b.Headroom() != 0 {
		t.Errorf("budget headroom = %d, want 0", b.Headroom())
	}
}

func TestHeadroom_ClimbsWhileThroughputHolds(t *testing.T) {
	c, _ := newTestController(t, 100, 0)

	// A steady 400 MB/s: every tick moves the same bytes, so the link is not
	// suffering and the controller should keep taking slack.
	const perTick = 400 << 20 * 10 // 10s at 400 MB/s
	var total int64
	stepAt(c, 0, 0, 1)

	var reserves []int
	for i := 1; i <= 4; i++ {
		total += perTick
		reserves = append(reserves, stepAt(c, i*10, total, 1))
	}
	for i := 1; i < len(reserves); i++ {
		if reserves[i] <= reserves[i-1] {
			t.Fatalf("reserve did not climb on flat throughput: %v", reserves)
		}
	}
}

func TestHeadroom_BacksOffWhenThroughputDrops(t *testing.T) {
	c, _ := newTestController(t, 100, 0)

	const good = 400 << 20 * 10
	var total int64
	stepAt(c, 0, 0, 1)
	total += good
	stepAt(c, 10, total, 1)
	total += good
	peak := stepAt(c, 20, total, 1)

	// Now throughput falls 20% — well past the free-only allowance.
	total += good * 80 / 100
	got := stepAt(c, 30, total, 1)

	if got >= peak {
		t.Errorf("reserve = %d after a 20%% throughput drop, want less than %d", got, peak)
	}
	if c.ceiling == 0 {
		t.Error("controller should have recorded a ceiling after backing off")
	}
}

func TestHeadroom_SpendPolicyToleratesConfiguredLoss(t *testing.T) {
	free, _ := newTestController(t, 100, 0)
	spend, _ := newTestController(t, 100, 15)

	// A 9% drop: past the free-only dead band, inside a 15% spend budget.
	drive := func(c *headroomController) int {
		const good = 400 << 20 * 10
		var total int64
		stepAt(c, 0, 0, 1)
		total += good
		stepAt(c, 10, total, 1)
		total += good
		peak := stepAt(c, 20, total, 1)
		total += good * 91 / 100
		return stepAt(c, 30, total, 1) - peak
	}

	if d := drive(free); d >= 0 {
		t.Errorf("free-only policy should back off on a 9%% drop, moved %+d", d)
	}
	if d := drive(spend); d < 0 {
		t.Errorf("spend policy (15%%) should tolerate a 9%% drop, moved %+d", d)
	}
}

func TestHeadroom_ClampedToPoolFraction(t *testing.T) {
	c, b := newTestController(t, 100, 0)

	const good = 400 << 20 * 10
	var total int64
	stepAt(c, 0, 0, 1)
	// Drive many flat ticks; the reserve must stop at the clamp, never starve import.
	for i := 1; i <= 40; i++ {
		total += good
		stepAt(c, i*10, total, 1)
	}
	maxReserve := b.Capacity() / headroomMaxFraction
	if c.reserve > maxReserve {
		t.Errorf("reserve = %d, exceeds clamp of capacity/%d = %d",
			c.reserve, headroomMaxFraction, maxReserve)
	}
	if c.reserve == 0 {
		t.Error("reserve should have climbed to the clamp on permanently flat throughput")
	}
}

// TestHeadroom_ReappliesLearnedKneeOnNextStream pins the property that makes this
// useful for short playback: converging takes several ticks, so a stream that only
// lasts a couple of minutes would otherwise spend most of its life unprotected.
func TestHeadroom_ReappliesLearnedKneeOnNextStream(t *testing.T) {
	c, _ := newTestController(t, 100, 0)

	const good = 400 << 20 * 10
	var total int64
	stepAt(c, 0, 0, 1)
	for i := 1; i <= 3; i++ {
		total += good
		stepAt(c, i*10, total, 1)
	}
	// learned deliberately trails reserve by one step: a tick's throughput
	// reading reflects the reserve that was in force during the PRECEDING
	// interval, so only that reserve has been proven not to cost throughput.
	// The step currently installed is speculative and must not be carried into
	// the next stream as if it were validated.
	learned := c.learned
	if learned == 0 {
		t.Fatal("expected the controller to have learned a non-zero reserve")
	}
	if learned >= c.reserve {
		t.Fatalf("learned %d should trail the speculative reserve %d", learned, c.reserve)
	}

	// Stream ends, reserve released.
	total += good
	if got := stepAt(c, 40, total, 0); got != 0 {
		t.Fatalf("reserve = %d after streams ended, want 0", got)
	}

	// New stream: protection must be back immediately, not after another climb.
	total += good
	if got := stepAt(c, 50, total, 1); got != learned {
		t.Errorf("reserve = %d on the first tick of a new stream, want the learned %d", got, learned)
	}
}
