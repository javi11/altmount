package segcache

import (
	"context"
	"log/slog"
	"math"
	"runtime/metrics"
	"time"
)

// The memory tier is the one large, elastic block of live heap. When the Go
// soft memory limit sits below what the process holds live, the collector
// runs back to back and the GC CPU limiter engages: CPU climbs to half the
// machine and stays there for as long as the stream runs, while no memory is
// freed because it is all live. The pressure governor watches for that state
// and shrinks the tier so read-ahead windows and connection buffers fit
// under the limit again, then restores it once the heap has been calm for a
// while. RAM stays bounded by the limit; CPU never spirals.
const (
	pressureInterval = 2 * time.Second
	// pressureHighFraction of the limit held live is treated as pressure
	// before the limiter has to engage.
	pressureHighFraction = 0.90
	// pressureLowFraction of the limit is calm enough to start recovering.
	pressureLowFraction = 0.70
	// pressureRecoverAfter is how long the heap must stay calm per step back up.
	pressureRecoverAfter = 60 * time.Second
	// pressureSteps is how many steps take the tier from full to the floor.
	pressureSteps = 4
	// pressureFloor keeps a small tier for warm opens and fan-out sharing.
	pressureFloor = int64(32) << 20
)

// noCeiling means the configured capacity applies unchanged.
const noCeiling int64 = -1

type pressureSample struct {
	limiterCycle uint64 // /gc/limiter/last-enabled:gc-cycle
	live         int64  // /gc/heap/live:bytes (marked heap of the last cycle; 0 before the first)
	limit        int64  // /gc/gomemlimit:bytes
}

type pressureGovernor struct {
	full      func() int64 // configured memory tier capacity
	ceiling   int64
	lastCycle uint64
	baselined bool
	calmSince time.Time
}

func newPressureGovernor(full func() int64) *pressureGovernor {
	return &pressureGovernor{full: full, ceiling: noCeiling}
}

// step folds one sample into the governor and returns the ceiling to apply
// and whether it changed. Pure so the policy is testable without a runtime.
func (g *pressureGovernor) step(s pressureSample, now time.Time) (int64, bool) {
	engaged := g.baselined && s.limiterCycle != g.lastCycle
	g.lastCycle, g.baselined = s.limiterCycle, true

	full := g.full()
	if s.limit <= 0 || s.limit == math.MaxInt64 || full <= pressureFloor {
		return g.set(noCeiling)
	}
	step := full / pressureSteps
	current := g.ceiling
	if current == noCeiling || current > full {
		current = full
	}
	switch {
	case engaged || float64(s.live) > float64(s.limit)*pressureHighFraction:
		g.calmSince = now // recovery is counted from the last shrink
		return g.set(g.normalize(max(current-step, pressureFloor), full))
	case float64(s.live) < float64(s.limit)*pressureLowFraction:
		if current >= full {
			return g.set(noCeiling)
		}
		if g.calmSince.IsZero() {
			g.calmSince = now
			return g.set(g.normalize(current, full))
		}
		if now.Sub(g.calmSince) < pressureRecoverAfter {
			return g.set(g.normalize(current, full))
		}
		g.calmSince = now
		return g.set(g.normalize(current+step, full))
	default:
		g.calmSince = time.Time{}
		return g.set(g.normalize(current, full))
	}
}

func (g *pressureGovernor) normalize(c, full int64) int64 {
	if c >= full {
		return noCeiling
	}
	return c
}

func (g *pressureGovernor) set(c int64) (int64, bool) {
	changed := c != g.ceiling
	g.ceiling = c
	return c, changed
}

var pressureMetricNames = []string{
	"/gc/limiter/last-enabled:gc-cycle",
	"/gc/heap/live:bytes",
	"/gc/gomemlimit:bytes",
}

func readPressureSample() (pressureSample, bool) {
	samples := make([]metrics.Sample, len(pressureMetricNames))
	for i, n := range pressureMetricNames {
		samples[i].Name = n
	}
	metrics.Read(samples)
	for _, s := range samples {
		if s.Value.Kind() != metrics.KindUint64 {
			return pressureSample{}, false
		}
	}
	return pressureSample{
		limiterCycle: samples[0].Value.Uint64(),
		live:         clampInt64(samples[1].Value.Uint64()),
		limit:        clampInt64(samples[2].Value.Uint64()),
	}, true
}

func clampInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// RunPressureGovernor shrinks the memory tier while the Go soft memory limit
// is under pressure and restores it when the heap is calm. Blocks until ctx
// is cancelled; run it in its own goroutine.
func (s *Source) RunPressureGovernor(ctx context.Context) {
	g := newPressureGovernor(func() int64 { return s.getCfg().SegmentCache.MemoryBytes() })
	t := time.NewTicker(pressureInterval)
	defer t.Stop()
	warned := false
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			sample, ok := readPressureSample()
			if !ok {
				slog.ErrorContext(ctx, "Runtime GC metrics unavailable; memory pressure governor disabled")
				return
			}
			ceiling, changed := g.step(sample, now)
			if !changed {
				continue
			}
			s.SetMemoryCeiling(ceiling)
			attrs := []any{
				"live_mb", sample.live >> 20, "limit_mb", sample.limit >> 20,
				"tier_mb", s.effectiveMemoryBytes(s.getCfg().SegmentCache.MemoryBytes()) >> 20,
			}
			switch {
			case ceiling == noCeiling:
				warned = false // a later episode is news again
				slog.InfoContext(ctx, "Memory pressure eased; segment cache memory tier restored", attrs...)
			case !warned:
				warned = true
				slog.WarnContext(ctx, "Live heap is pressing on the Go memory limit; shrinking the segment cache memory tier to keep GC CPU down. Raise memory_limit_mb or lower segment_cache.memory_mb to avoid this.", attrs...)
			default:
				slog.InfoContext(ctx, "Memory pressure persists; segment cache memory tier shrunk further", attrs...)
			}
		}
	}
}
