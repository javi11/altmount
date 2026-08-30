package segcache

import (
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOperationsBlockUntilLoaded verifies that Put/Get/Has block until LoadCatalog completes,
// ensuring no cache misses or race conditions during boot hydration.
func TestOperationsBlockUntilLoaded(t *testing.T) {
	cfg := Config{CachePath: t.TempDir(), MaxSizeBytes: 10 * 1024 * 1024}
	c, err := NewSegmentCache(cfg, slog.Default())
	require.NoError(t, err)

	var (
		wg      sync.WaitGroup
		putDone = make(chan struct{})
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := c.Put("busy@msg", []byte("data"))
		assert.NoError(t, err)
		close(putDone)
	}()

	// Ensure the goroutine is blocked waiting for hydration
	select {
	case <-putDone:
		t.Fatal("Put should have blocked until LoadCatalog")
	case <-time.After(50 * time.Millisecond):
	}

	// Now hydrate the catalog
	c.LoadCatalog()

	// Put should complete
	wg.Wait()
	assert.True(t, c.Has("busy@msg"))
	assert.EqualValues(t, 1, c.ItemCount())
}

// TestOperationsProceedWhenHydrationTimesOut guards against deadlocks if LoadCatalog is never called.
func TestOperationsProceedWhenHydrationTimesOut(t *testing.T) {
	cfg := Config{
		CachePath:        t.TempDir(),
		MaxSizeBytes:     10 * 1024 * 1024,
		HydrationTimeout: 20 * time.Millisecond,
	}
	c, err := NewSegmentCache(cfg, slog.Default())
	require.NoError(t, err)

	// Put without calling LoadCatalog: should unblock after HydrationTimeout and succeed.
	err = c.Put("unhydrated@msg", []byte("data"))
	assert.NoError(t, err)
	assert.True(t, c.Has("unhydrated@msg"))
}
