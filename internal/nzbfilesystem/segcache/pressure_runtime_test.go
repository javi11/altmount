package segcache

import (
	"runtime"
	"runtime/debug"
	"testing"
	"time"
)

func TestReadPressureSampleReportsRuntimeLimit(t *testing.T) {
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	const limit = int64(4) << 30
	debug.SetMemoryLimit(limit)
	runtime.GC() // live bytes are the marked heap of the last cycle

	s, ok := readPressureSample()
	if !ok {
		t.Fatal("runtime metrics missing; the governor would be inert")
	}
	if s.limit != limit {
		t.Fatalf("limit = %d, want %d", s.limit, limit)
	}
	if s.live <= 0 {
		t.Fatalf("live heap = %d, want > 0", s.live)
	}
}

// A real limit below the real live heap must make the governor shrink the
// tier on the next sample, without waiting for the GC CPU limiter.
func TestGovernorShrinksUnderRealHeapPressure(t *testing.T) {
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	debug.SetMemoryLimit(int64(64) << 30)
	runtime.GC()
	base, _ := readPressureSample()

	pinned := make([]byte, 128<<20)
	for i := range pinned {
		pinned[i] = byte(i)
	}
	runtime.GC()
	// Limit just above the live heap so live/limit is well past pressureHighFraction.
	debug.SetMemoryLimit(base.live + 128<<20 + 4<<20)

	g := newTestGovernor()
	s, ok := readPressureSample()
	if !ok {
		t.Fatal("metrics unavailable")
	}
	g.step(s, time.Now())
	s, _ = readPressureSample()
	c, changed := g.step(s, time.Now())
	if !changed || c == noCeiling {
		t.Fatalf("live=%d limit=%d: ceiling=%d changed=%v, want a shrink", s.live, s.limit, c, changed)
	}
	runtime.KeepAlive(pinned)
}
