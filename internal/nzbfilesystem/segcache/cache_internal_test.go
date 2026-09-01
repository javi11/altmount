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

// TestLoadCatalogKeepsEntriesFromEscapedPuts verifies that a Put that proceeded
// past the hydration timeout is not clobbered when LoadCatalog later hydrates,
// which would orphan its .seg file on disk.
func TestLoadCatalogKeepsEntriesFromEscapedPuts(t *testing.T) {
	dir := t.TempDir()

	seed, err := NewSegmentCache(Config{CachePath: dir, MaxSizeBytes: 10 * 1024 * 1024}, slog.Default())
	require.NoError(t, err)
	seed.LoadCatalog()
	require.NoError(t, seed.Put("old@msg", []byte("old-data")))
	require.NoError(t, seed.SaveCatalog())

	c, err := NewSegmentCache(Config{
		CachePath:        dir,
		MaxSizeBytes:     10 * 1024 * 1024,
		HydrationTimeout: 20 * time.Millisecond,
	}, slog.Default())
	require.NoError(t, err)

	// Escapes waitReady via the timeout, landing in the map before hydration.
	require.NoError(t, c.Put("escaped@msg", []byte("escaped-data")))

	c.LoadCatalog()

	assert.True(t, c.Has("escaped@msg"))
	assert.True(t, c.Has("old@msg"))
	assert.EqualValues(t, 2, c.ItemCount())
	assert.EqualValues(t, int64(len("old-data")+len("escaped-data")), c.TotalSize())
}
