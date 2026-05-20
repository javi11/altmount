package nzbfilesystem

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// disconnect_storm_test.go pins the post-S9 invariant for HTTP-client
// disconnects: mvf.Close interrupts the in-flight UsenetReader via an
// atomic interruptHandle BEFORE trying to acquire mvf.mu. The blocked
// segment download returns ctx.Canceled, the concurrent Read releases
// the lock, and Close completes in microseconds regardless of segment
// latency.
//
// Without this, a Plex/Jellyfin scrub session that produces frequent
// disconnect cycles would pin a request goroutine (and its pool slot)
// for the per-attempt timeout × retry-count on every disconnect.

// TestStorm_ClientDisconnectHoldsPoolSlotForUpTo30s drives the
// disconnect scenario directly. Each "session" opens a MetadataVirtualFile,
// reads a few bytes (which kicks off prefetch goroutines holding
// BodyPriority calls in flight), cancels the read context (simulating
// HTTP client disconnect), and times how long mvf.Close takes.
//
// PINNED INVARIANT (post-S9 fix): cancellation propagates fast.
// mvf.Close calls interruptCurrentReader before contending for mvf.mu,
// which fires ctx-cancel on the in-flight UsenetReader. The blocked
// segment download returns ctx.Canceled, the concurrent Read releases
// mvf.mu, and Close completes in microseconds regardless of segment
// latency. Test asserts < 250ms with a 2s fake-pool latency.
func TestStorm_ClientDisconnectHoldsPoolSlotForUpTo30s(t *testing.T) {
	t.Parallel()
	const (
		segCount    = 5
		segSize     = 1024
		maxPrefetch = 4
		// Slow segment latency: simulates a real provider taking time
		// to respond. mvf.Close waits for this to complete.
		segLatency = 2 * time.Second
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fp := fakepool.New()
	configurePoolForFile(fp, segCount, segSize, fakepool.SegmentBehavior{
		Latency: segLatency,
	})

	mvf := newTestMVF(t, ctx, fp, segCount, segSize, maxPrefetch)

	// Start a Read in a goroutine. This triggers reader initialization
	// and downloadManager spawning. The Read itself will block waiting
	// for segment 0's body to arrive.
	readDone := make(chan struct{})
	readStarted := make(chan struct{})
	go func() {
		defer close(readDone)
		close(readStarted)
		buf := make([]byte, 16)
		_, _ = mvf.Read(buf)
	}()
	<-readStarted
	// Give Read time to acquire mvf.mu and kick off prefetch. Without
	// this delay, mvf.Close could acquire the lock first and nil out
	// meta before Read's ensureReader runs.
	time.Sleep(100 * time.Millisecond)

	// Wait until at least one BodyPriority call is in flight, confirming
	// the prefetch pipeline is alive.
	deadline := time.Now().Add(2 * time.Second)
	for fp.InFlight() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fp.InFlight() == 0 {
		t.Fatalf("no BodyPriority call ever went in flight; prefetch may not be running")
	}

	// "Disconnect": close the file. This calls mvf.Close, which calls
	// UsenetReader.Close, which waits up to 30s for in-flight downloads.
	// Time how long it takes.
	closeStart := time.Now()
	_ = mvf.Close()
	closeElapsed := time.Since(closeStart)
	<-readDone // let the orphaned Read goroutine exit so race detector is happy

	t.Logf("mvf.Close took %s after disconnect with %s segment latency "+
		"(invariant: < %s regardless of segment latency)",
		closeElapsed, segLatency, 250*time.Millisecond)

	// PINNED INVARIANT: Close interrupts in-flight downloads instead of
	// waiting for them. Bound is loose to absorb scheduler jitter; the
	// real number is in the microseconds on a healthy host.
	const closeBudget = 250 * time.Millisecond
	if closeElapsed > closeBudget {
		t.Errorf("INVARIANT regression (S9): mvf.Close took %s, want <= %s with segLatency=%s. "+
			"interruptCurrentReader is no longer cancelling the in-flight UsenetReader before "+
			"contending for mvf.mu.",
			closeElapsed, closeBudget, segLatency)
	}
}

// TestStorm_ConcurrentDisconnectsPinManyGoroutines is the cumulative
// version of the single-disconnect scenario above. It opens 10 concurrent
// "sessions", each starts streaming, each disconnects after a short
// delay.
//
// PINNED INVARIANT (post-S9 fix): max per-close latency stays under a
// small constant regardless of segLatency. The close path interrupts
// in-flight downloads via the atomic interruptHandle rather than
// waiting for them.
func TestStorm_ConcurrentDisconnectsPinManyGoroutines(t *testing.T) {
	t.Parallel()
	const (
		sessions    = 10
		segCount    = 5
		segSize     = 1024
		maxPrefetch = 4
		segLatency  = 1500 * time.Millisecond
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fp := fakepool.New()
	for i := 0; i < segCount; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{
			Latency: segLatency,
			Bytes:   segments.Payload(i, segSize),
		})
	}

	var wg sync.WaitGroup
	var maxCloseElapsed time.Duration
	var mu sync.Mutex

	overallStart := time.Now()
	for s := 0; s < sessions; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mvf := newTestMVF(t, ctx, fp, segCount, segSize, maxPrefetch)

			// Signal when this session's Read has actually acquired
			// mvf.mu and reached the in-flight wait. Using a per-session
			// signal (not the shared fp.InFlight counter) avoids races
			// where one session's Close runs before its own Read has
			// started.
			readStarted := make(chan struct{})
			readDone := make(chan struct{})
			go func() {
				defer close(readDone)
				close(readStarted) // signals before Read actually acquires lock
				buf := make([]byte, 16)
				_, _ = mvf.Read(buf)
			}()
			<-readStarted
			// Give the Read goroutine a moment to acquire mvf.mu and
			// kick off the prefetch pipeline.
			time.Sleep(50 * time.Millisecond)

			closeStart := time.Now()
			_ = mvf.Close()
			elapsed := time.Since(closeStart)
			<-readDone

			mu.Lock()
			if elapsed > maxCloseElapsed {
				maxCloseElapsed = elapsed
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	overallElapsed := time.Since(overallStart)

	t.Logf("%d concurrent disconnects; max close=%s overall=%s "+
		"(invariant: max close < %s regardless of segLatency=%s)",
		sessions, maxCloseElapsed, overallElapsed, 250*time.Millisecond, segLatency)

	// PINNED INVARIANT: max close < 250ms regardless of segLatency.
	const closeBudget = 250 * time.Millisecond
	if maxCloseElapsed > closeBudget {
		t.Errorf("INVARIANT regression (S9): maxCloseElapsed=%s, want <= %s after %d concurrent "+
			"disconnects with segLatency=%s. interruptCurrentReader is no longer cancelling "+
			"in-flight UsenetReaders before contending for mvf.mu.",
			maxCloseElapsed, closeBudget, sessions, segLatency)
	}
}
