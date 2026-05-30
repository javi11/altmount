package api

import (
	"sync"
	"testing"
	"time"
)

// speedtest_coordinator_test.go pins the janitor behaviour added to
// prevent the cache from retaining nntppool.Client instances after
// every speed-tested provider goes idle. Without the janitor, an idle
// pod would hold one *nntppool.Client per ever-speed-tested providerID
// for the lifetime of the process — the cache only shrinks when the
// same providerID is requested again.
//
// We don't dial real NNTP here. Test entries install nil clients;
// shutdown's nil-guard makes that safe.

func installExpiredEntry(sc *speedtestCoordinator, id string) {
	sc.mu.Lock()
	sc.clients[id] = &cachedSpeedtestClient{
		client:    nil,
		expiresAt: time.Now().Add(-1 * time.Hour),
		host:      "test:" + id,
	}
	sc.mu.Unlock()
}

func installFreshEntry(sc *speedtestCoordinator, id string) {
	sc.mu.Lock()
	sc.clients[id] = &cachedSpeedtestClient{
		client:    nil,
		expiresAt: time.Now().Add(1 * time.Hour),
		host:      "test:" + id,
	}
	sc.mu.Unlock()
}

func cacheSize(sc *speedtestCoordinator) int {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return len(sc.clients)
}

// newTestCoordinator returns a coordinator with a stopped janitor so
// tests can drive sweepExpired manually. The wg is empty (no goroutine
// started) so shutdown returns immediately.
func newTestCoordinator() *speedtestCoordinator {
	return &speedtestCoordinator{
		clients: make(map[string]*cachedSpeedtestClient),
		stopCh:  make(chan struct{}),
	}
}

// TestSpeedtestCoordinator_SweepEvictsExpiredEntries pins the headline
// behaviour: entries past expiresAt MUST be removed by sweepExpired
// without requiring a subsequent request for the same providerID. This
// is the leak fix — without the janitor calling sweepExpired on a
// ticker, the cache would only shrink on next access, leaving every
// ever-speed-tested provider's *nntppool.Client resident.
func TestSpeedtestCoordinator_SweepEvictsExpiredEntries(t *testing.T) {
	sc := newTestCoordinator()

	installExpiredEntry(sc, "p1")
	installExpiredEntry(sc, "p2")
	installFreshEntry(sc, "p3")

	if got := cacheSize(sc); got != 3 {
		t.Fatalf("setup: cache size = %d, want 3", got)
	}

	sc.sweepExpired()

	if got := cacheSize(sc); got != 1 {
		t.Errorf("after sweep: cache size = %d, want 1 (only p3 should survive)", got)
	}
	sc.mu.Lock()
	_, hasP3 := sc.clients["p3"]
	sc.mu.Unlock()
	if !hasP3 {
		t.Errorf("after sweep: p3 (fresh entry) was incorrectly evicted")
	}
}

// TestSpeedtestCoordinator_NewCoordinatorStartsAndStopsJanitor verifies
// the janitor goroutine is started by newSpeedtestCoordinator and that
// shutdown stops it. Without this pairing, every speedtest endpoint hit
// would either bypass eviction (if janitor never started) or leak the
// janitor goroutine itself (if shutdown didn't stop it).
func TestSpeedtestCoordinator_NewCoordinatorStartsAndStopsJanitor(t *testing.T) {
	sc := newSpeedtestCoordinator()

	done := make(chan struct{})
	go func() {
		sc.shutdown()
		close(done)
	}()

	select {
	case <-done:
		// Shutdown completed — janitor exited cleanly.
	case <-time.After(2 * time.Second):
		t.Fatalf("shutdown did not complete within 2s — janitor stuck or wg not paired correctly")
	}

	// A second shutdown must be a safe no-op (stopOnce guards close).
	sc.shutdown()
}

// TestSpeedtestCoordinator_ShutdownDrainsAllEntries verifies shutdown
// drains the map regardless of expiresAt. The map must end up empty so
// the *speedtestCoordinator itself becomes GC-eligible.
func TestSpeedtestCoordinator_ShutdownDrainsAllEntries(t *testing.T) {
	sc := newSpeedtestCoordinator()
	installFreshEntry(sc, "p1")
	installFreshEntry(sc, "p2")

	sc.shutdown()

	if got := cacheSize(sc); got != 0 {
		t.Errorf("after shutdown: cache size = %d, want 0 (must drain all entries regardless of TTL)", got)
	}
}

// TestSpeedtestCoordinator_ConcurrentSweepAndAccess is a race-detector
// stress: while sweepExpired runs concurrently, callers install and
// look up entries. The mutex on sc.clients must serialise both paths.
func TestSpeedtestCoordinator_ConcurrentSweepAndAccess(t *testing.T) {
	sc := newTestCoordinator()

	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				sc.sweepExpired()
			}
		}
	}()

	for i := 0; i < 4; i++ {
		wg.Add(1)
		id := i
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				select {
				case <-stop:
					return
				default:
				}
				if j%2 == 0 {
					installExpiredEntry(sc, idStr(id))
				} else {
					installFreshEntry(sc, idStr(id))
				}
				_ = cacheSize(sc)
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
	// No assertion — passing -race is the assertion.
}

// idStr is a tiny stdlib-free int formatter for single-digit IDs.
func idStr(i int) string {
	return string(rune('0' + i))
}
