package validation

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/javi11/nntppool/v4"
)

// delayedStatClient answers every STAT with the configured outcome after a
// per-id delay that applies only to the first STAT of that id; a re-issued STAT
// answers immediately, the way a hedge landing on an idle connection does.
type delayedStatClient struct {
	*scriptedStatClient
	firstDelay map[string]time.Duration
	sweeps     int
}

func newDelayedStatClient(outcomes map[string][]error, firstDelay map[string]time.Duration) *delayedStatClient {
	return &delayedStatClient{
		scriptedStatClient: newScriptedStatClient(outcomes),
		firstDelay:         firstDelay,
	}
}

func (c *delayedStatClient) StatMany(ctx context.Context, ids []string, _ nntppool.StatManyOptions) <-chan nntppool.StatManyResult {
	out := make(chan nntppool.StatManyResult, len(ids))
	c.mu.Lock()
	c.sweeps++
	c.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			c.mu.Lock()
			attempt := c.calls[id]
			c.calls[id]++
			sequence := c.outcomes[id]
			var err error
			if len(sequence) > 0 {
				err = sequence[min(attempt, len(sequence)-1)]
			}
			delay := time.Duration(0)
			if attempt == 0 {
				delay = c.firstDelay[id]
			}
			c.mu.Unlock()

			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			result := nntppool.StatManyResult{MessageID: id, Err: err}
			if err == nil {
				result.Result = &nntppool.StatResult{MessageID: id}
			}
			select {
			case out <- result:
			case <-ctx.Done():
			}
		}(id)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func (c *delayedStatClient) sweepCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sweeps
}

func probeFile(count int) []FastFailFile {
	return []FastFailFile{{Filename: "movie.mkv", Segments: makeTestSegments("seg", count)}}
}

func TestFastFailReleaseProbeHedgesStragglerStat(t *testing.T) {
	straggler := "seg-40"
	client := newDelayedStatClient(nil, map[string]time.Duration{straggler: 5 * time.Second})

	start := time.Now()
	missing, err := FastFailReleaseProbe(context.Background(), probeFile(64), fastFailPoolManager{client: client}, 100, 64, 30*time.Second, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("FastFailReleaseProbe error = %v, want nil", err)
	}
	if missing {
		t.Fatal("missing = true, want false: every article exists")
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("probe took %s, want the straggler hedged well inside the 2 s attempt ceiling", elapsed)
	}
	if got := client.callCount(straggler); got != 2 {
		t.Fatalf("straggler STATs = %d, want 2 (original + hedge)", got)
	}
	if got := client.callCount("seg-0"); got != 1 {
		t.Fatalf("fast id STATs = %d, want 1: only stragglers are hedged", got)
	}
}

func TestFastFailReleaseProbeDoesNotHedgeUniformlySlowSweep(t *testing.T) {
	delays := make(map[string]time.Duration, 64)
	for i := range 64 {
		delays[fmt.Sprintf("seg-%d", i)] = 400 * time.Millisecond
	}
	client := newDelayedStatClient(nil, delays)

	missing, err := FastFailReleaseProbe(context.Background(), probeFile(64), fastFailPoolManager{client: client}, 100, 64, 30*time.Second, nil)
	if err != nil {
		t.Fatalf("FastFailReleaseProbe error = %v, want nil", err)
	}
	if missing {
		t.Fatal("missing = true, want false")
	}
	if got := client.sweepCount(); got != 1 {
		t.Fatalf("StatMany sweeps = %d, want 1: a uniformly slow provider has no stragglers to hedge", got)
	}
}

func TestFastFailReleaseProbeHedgedMissIsDefinitive(t *testing.T) {
	straggler := "seg-40"
	client := newDelayedStatClient(
		map[string][]error{straggler: {nntppool.ErrArticleNotFound}},
		map[string]time.Duration{straggler: 5 * time.Second},
	)

	start := time.Now()
	missing, err := FastFailReleaseProbe(context.Background(), probeFile(64), fastFailPoolManager{client: client}, 100, 64, 30*time.Second, nil)
	if err != nil {
		t.Fatalf("FastFailReleaseProbe error = %v, want nil for definitive miss", err)
	}
	if !missing {
		t.Fatal("missing = false, want true: the hedged STAT answered 430")
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("probe took %s, want the hedged 430 to settle it early", elapsed)
	}
}

func TestFastFailReleaseProbeHedgeRespectsCancellation(t *testing.T) {
	client := newDelayedStatClient(nil, map[string]time.Duration{"seg-40": 5 * time.Second})
	// Every hedge answers immediately, so the only way this probe finishes fast
	// AND reports cancellation is if the straggler wait observed ctx.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := FastFailReleaseProbe(ctx, probeFile(64), fastFailPoolManager{client: client}, 100, 64, 30*time.Second, nil)
	if err == nil {
		t.Fatal("FastFailReleaseProbe error = nil, want caller cancellation to surface")
	}
}
