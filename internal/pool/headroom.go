package pool

import (
	"context"
	"log/slog"
	"time"
)

// The adaptive import headroom controller.
//
// On a link that is already saturated, the pool has slack: connections that are
// not converting into bytes because the wire, not the pool, is the limit.
// Handing that slack to playback costs nothing measurable, and measurably helps
// — at 100 connections behind a 400 MB/s link, raising the per-stream reserve
// from 2 to 8 improved stream throughput 6.5% while import throughput moved
// 0.5%. Push further and the pool can no longer fill the link: at a reserve of
// 32, aggregate throughput fell 389 -> 353 MB/s.
//
// That last sentence is the whole control law. Aggregate throughput holding
// flat as the reserve grows means the reserve is still coming out of slack;
// aggregate throughput falling means it has started coming out of the link.
// The controller climbs while the former holds and stops at the knee.
//
// The same rule is safe on an unsaturated link. There, taking connections away
// from import reduces throughput immediately, so the controller stops at once
// and behaves exactly as the fixed default did.
const (
	// headroomStep is how many connections per stream each adjustment moves.
	headroomStep = 4

	// headroomDeadband is the fraction of aggregate throughput that must be
	// lost before a reading counts as a real drop rather than noise. Runs of
	// the benchmark varied by well under 1% between repetitions.
	headroomDeadband = 0.03

	// headroomMaxFraction clamps the reserve to capacity/N, so a pathological
	// reading can never starve import of the pool.
	headroomMaxFraction = 4

	// headroomInterval is how often the controller samples. Long enough that a
	// step's effect shows up in the throughput EWMA before the next decision.
	headroomInterval = 15 * time.Second
)

// HeadroomPolicy decides when the controller stops climbing.
type HeadroomPolicy struct {
	// MaxTotalCostPct is how much aggregate pool throughput may be given up to
	// protect playback. Zero — the default — means "free only": stop at the
	// knee and never trade throughput for latency, which is the safe default
	// for a deployment whose link rate is unknown.
	//
	// Setting it non-zero buys the larger win. Measured at 400 MB/s: allowing a
	// ~9% aggregate cost took stream p50 from 190ms to 146ms and p99 from 245ms
	// to 167ms — playback at 95% of its completely-unloaded speed while a full
	// import ran.
	MaxTotalCostPct int
}

// headroomController adjusts an ImportBudget's per-stream reserve from observed
// aggregate throughput. It is driven by step, which is pure with respect to
// time and the byte counter so tests can drive it without sleeping.
type headroomController struct {
	budget *ImportBudget
	policy HeadroomPolicy
	logger *slog.Logger

	lastBytes int64
	lastAt    time.Time
	haveLast  bool

	// reference is the aggregate rate observed at the lowest reserve tried this
	// session — what "no worse than before" is measured against.
	reference float64

	reserve int
	ceiling int // reserve known to cost throughput; 0 = not yet found
	learned int // best known-good reserve, carried across stream sessions
}

func newHeadroomController(budget *ImportBudget, policy HeadroomPolicy) *headroomController {
	return &headroomController{
		budget: budget,
		policy: policy,
		logger: slog.Default().With("component", "pool-headroom"),
	}
}

// step folds one observation into the controller and applies the result.
// totalBytes is a monotonic count of bytes the pool has consumed.
func (c *headroomController) step(now time.Time, totalBytes int64, streams int) {
	if streams == 0 {
		// Nothing to protect: hand the whole pool back to import. The learned
		// reserve survives so the next stream is protected from its first tick
		// rather than after another slow climb.
		c.apply(0)
		c.haveLast = false
		c.reference = 0
		return
	}

	if !c.haveLast {
		c.lastBytes, c.lastAt, c.haveLast = totalBytes, now, true
		c.apply(c.learned)
		return
	}

	dt := now.Sub(c.lastAt).Seconds()
	delta := totalBytes - c.lastBytes
	c.lastBytes, c.lastAt = totalBytes, now
	if dt <= 0 || delta <= 0 {
		return
	}
	rate := float64(delta) / dt

	if c.reference == 0 {
		c.reference = rate
		c.apply(c.reserve + headroomStep)
		return
	}

	allowed := float64(c.policy.MaxTotalCostPct)/100 + headroomDeadband
	drop := (c.reference - rate) / c.reference

	switch {
	case drop > allowed:
		// The reserve has started coming out of the link rather than out of
		// slack. Record where that happened and step back under it.
		c.ceiling = c.reserve
		next := c.reserve - headroomStep
		if next < 0 {
			next = 0
		}
		c.learned = next
		c.apply(next)
	case c.ceiling != 0 && c.reserve+headroomStep >= c.ceiling:
		// Sitting just below a known ceiling: hold rather than oscillate across it.
		c.learned = c.reserve
	default:
		c.learned = c.reserve
		c.apply(c.reserve + headroomStep)
	}
}

// apply clamps and installs a reserve.
func (c *headroomController) apply(reserve int) {
	if maxReserve := c.budget.Capacity() / headroomMaxFraction; maxReserve > 0 && reserve > maxReserve {
		reserve = maxReserve
	}
	if reserve < 0 {
		reserve = 0
	}
	if reserve == c.reserve {
		return
	}
	c.reserve = reserve
	c.budget.SetHeadroom(reserve)
}

// Run drives the controller until ctx is cancelled. totalBytes must return a
// monotonic byte counter and the current active-stream count.
func (c *headroomController) Run(ctx context.Context, sample func() (bytes int64, streams int, ok bool)) {
	t := time.NewTicker(headroomInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			bytes, streams, ok := sample()
			if !ok {
				continue // no pool or no stats yet; hold the current reserve
			}
			before := c.reserve
			c.step(now, bytes, streams)
			if c.reserve != before {
				c.logger.DebugContext(ctx, "Adjusted import headroom",
					"from", before, "to", c.reserve, "streams", streams, "ceiling", c.ceiling)
			}
		}
	}
}
