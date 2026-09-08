package validation

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/javi11/nntppool/v4"
	"github.com/kipsilabs/altmount/internal/pool"
)

const (
	// hedgeReportedFraction is the share of a sweep that must have answered
	// before the remainder counts as straggling. On a busy pool up to a
	// seventh of a 64-STAT probe has been seen queued behind other traffic
	// while the rest answered in ~150 ms; a uniformly slow or dead release
	// never gets this far.
	hedgeReportedFraction = 0.75
	// hedgeGraceLatencyFactor scales the observed median STAT latency into the
	// grace a straggler gets before it is re-issued.
	hedgeGraceLatencyFactor = 3
	hedgeMinGrace           = 250 * time.Millisecond
	hedgeMaxGrace           = 750 * time.Millisecond
)

// hedgedStatMany runs one StatMany sweep over ids and, once most of it has
// answered, re-issues the ids still outstanding on a second sweep so a STAT
// queued behind slow traffic on one connection does not hold the whole attempt
// to its deadline. Each id is reported at most once, whichever sweep answers
// first; both sweeps are cancelled as soon as every id has reported.
func hedgedStatMany(ctx context.Context, client pool.NntpClient, ids []string, concurrency int) <-chan nntppool.StatManyResult {
	out := make(chan nntppool.StatManyResult, len(ids))
	go func() {
		defer close(out)
		sweepCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		start := time.Now()
		var mu sync.Mutex
		reported := make(map[string]struct{}, len(ids))
		latencies := make([]time.Duration, 0, len(ids))
		threshold := hedgeThreshold(len(ids))

		primary := client.StatMany(sweepCtx, ids, nntppool.StatManyOptions{Concurrency: concurrency})
		var hedge <-chan nntppool.StatManyResult
		var grace <-chan time.Time

		deliver := func(r nntppool.StatManyResult) {
			mu.Lock()
			if _, dup := reported[r.MessageID]; dup {
				mu.Unlock()
				return
			}
			reported[r.MessageID] = struct{}{}
			latencies = append(latencies, time.Since(start))
			done := len(reported)
			mu.Unlock()

			out <- r
			if done == len(ids) {
				cancel()
			} else if hedge == nil && grace == nil && done >= threshold {
				grace = time.After(hedgeGrace(latencies))
			}
		}

		for primary != nil || hedge != nil {
			select {
			case r, ok := <-primary:
				if !ok {
					primary = nil
					continue
				}
				deliver(r)
			case r, ok := <-hedge:
				if !ok {
					hedge = nil
					continue
				}
				deliver(r)
			case <-grace:
				grace = nil
				mu.Lock()
				stragglers := make([]string, 0, len(ids)-len(reported))
				for _, id := range ids {
					if _, ok := reported[id]; !ok {
						stragglers = append(stragglers, id)
					}
				}
				mu.Unlock()
				if len(stragglers) == 0 {
					continue
				}
				// The stragglers are, by construction, queued behind other
				// normal-lane traffic; a hedge on the same lane would join the
				// queue. The priority lane lets an idle connection pick these
				// few bodyless requests up ahead of it.
				hedge = client.StatMany(sweepCtx, stragglers, nntppool.StatManyOptions{
					Concurrency: len(stragglers),
					Priority:    true,
					Skip: func(id string) bool {
						mu.Lock()
						defer mu.Unlock()
						_, done := reported[id]
						return done
					},
				})
			}
		}
	}()
	return out
}

// hedgeThreshold is the reported count at which the rest of an n-id sweep is
// considered straggling. It is never below 2 and never n itself, so a one- or
// two-id sweep is simply waited out.
func hedgeThreshold(n int) int {
	t := int(float64(n)*hedgeReportedFraction + 0.999)
	if t >= n {
		return n
	}
	return max(t, 2)
}

// hedgeGrace turns the latencies observed so far into how long a straggler is
// given before it is re-issued: a few medians, clamped so a very fast provider
// is not hedged on jitter and a slow one is not waited out to the deadline.
func hedgeGrace(latencies []time.Duration) time.Duration {
	sorted := slices.Clone(latencies)
	slices.Sort(sorted)
	median := sorted[len(sorted)/2]
	return min(max(median*hedgeGraceLatencyFactor, hedgeMinGrace), hedgeMaxGrace)
}
