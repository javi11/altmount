package segcache

import (
	"math"
	"testing"
	"time"
)

const (
	testFull  = int64(256) << 20
	testLimit = int64(700) << 20
)

func newTestGovernor() *pressureGovernor {
	return newPressureGovernor(func() int64 { return testFull })
}

func sample(cycle uint64, liveFrac float64) pressureSample {
	return pressureSample{limiterCycle: cycle, live: int64(float64(testLimit) * liveFrac), limit: testLimit}
}

func TestPressureGovernorHoldsFullCapacityWhenCalm(t *testing.T) {
	g := newTestGovernor()
	now := time.Unix(0, 0)
	for i := 0; i < 5; i++ {
		if c, changed := g.step(sample(7, 0.5), now); changed || c != noCeiling {
			t.Fatalf("calm step %d: ceiling=%d changed=%v, want none", i, c, changed)
		}
		now = now.Add(pressureInterval)
	}
}

func TestPressureGovernorShrinksWhenLimiterEngages(t *testing.T) {
	g := newTestGovernor()
	now := time.Unix(0, 0)
	g.step(sample(7, 0.5), now) // baseline limiter cycle
	c, changed := g.step(sample(9, 0.5), now.Add(pressureInterval))
	if !changed || c != testFull-testFull/pressureSteps {
		t.Fatalf("after limiter engaged: ceiling=%d changed=%v, want %d", c, changed, testFull-testFull/pressureSteps)
	}
	// Still engaging: keeps stepping down to the floor, never below.
	for i := uint64(10); i < 30; i++ {
		c, _ = g.step(sample(i, 0.5), now)
	}
	if c != pressureFloor {
		t.Fatalf("ceiling after sustained pressure = %d, want floor %d", c, pressureFloor)
	}
}

func TestPressureGovernorShrinksWhenLiveHeapNearsLimit(t *testing.T) {
	g := newTestGovernor()
	now := time.Unix(0, 0)
	g.step(sample(1, 0.5), now)
	c, changed := g.step(sample(1, 0.95), now)
	if !changed || c != testFull-testFull/pressureSteps {
		t.Fatalf("near-limit step: ceiling=%d changed=%v", c, changed)
	}
}

func TestPressureGovernorFirstSampleIsBaselineNotPressure(t *testing.T) {
	g := newTestGovernor()
	if c, changed := g.step(sample(42, 0.5), time.Unix(0, 0)); changed || c != noCeiling {
		t.Fatalf("first sample must only record the limiter cycle, got ceiling=%d changed=%v", c, changed)
	}
}

func TestPressureGovernorRecoversAfterCalmWindow(t *testing.T) {
	g := newTestGovernor()
	now := time.Unix(0, 0)
	g.step(sample(1, 0.5), now)
	c, _ := g.step(sample(2, 0.5), now)
	shrunk := c
	// Calm but not yet for the whole recovery window: hold.
	now = now.Add(pressureRecoverAfter / 2)
	if c, changed := g.step(sample(2, 0.5), now); changed || c != shrunk {
		t.Fatalf("mid-window: ceiling=%d changed=%v, want hold at %d", c, changed, shrunk)
	}
	now = now.Add(pressureRecoverAfter/2 + time.Second)
	c, changed := g.step(sample(2, 0.5), now)
	if !changed || c != noCeiling {
		t.Fatalf("after calm window: ceiling=%d changed=%v, want restored to full", c, changed)
	}
}

func TestPressureGovernorMiddleBandResetsCalmTimer(t *testing.T) {
	g := newTestGovernor()
	now := time.Unix(0, 0)
	g.step(sample(1, 0.5), now)
	g.step(sample(2, 0.5), now) // shrink
	now = now.Add(pressureRecoverAfter - time.Second)
	g.step(sample(2, 0.8), now) // between low and high: not calm
	now = now.Add(2 * time.Second)
	if _, changed := g.step(sample(2, 0.5), now); changed {
		t.Fatal("calm timer must restart after a middle-band sample")
	}
}

func TestPressureGovernorReleasesWhenNoLimit(t *testing.T) {
	g := newTestGovernor()
	now := time.Unix(0, 0)
	g.step(sample(1, 0.5), now)
	g.step(sample(2, 0.5), now) // shrink
	s := pressureSample{limiterCycle: 2, live: 1 << 30, limit: math.MaxInt64}
	if c, changed := g.step(s, now); !changed || c != noCeiling {
		t.Fatalf("no limit: ceiling=%d changed=%v, want released", c, changed)
	}
}

func TestPressureGovernorFloorNeverAboveConfiguredCapacity(t *testing.T) {
	small := pressureFloor / 2
	g := newPressureGovernor(func() int64 { return small })
	now := time.Unix(0, 0)
	g.step(sample(1, 0.5), now)
	c, changed := g.step(sample(2, 0.5), now)
	if changed || c != noCeiling {
		t.Fatalf("a tier already below the floor must not be touched, got ceiling=%d changed=%v", c, changed)
	}
}
