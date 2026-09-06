package segcache_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/nzbfilesystem/segcache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCache(t *testing.T, maxBytes int64, expiry time.Duration) *segcache.SegmentCache {
	t.Helper()
	dir := t.TempDir()
	cfg := segcache.Config{
		CachePath:      dir,
		MaxSizeBytes:   maxBytes,
		ExpiryDuration: expiry,
	}
	c, err := segcache.NewSegmentCache(cfg, slog.Default())
	require.NoError(t, err)
	// Hydrate + clear the loading gate; mirrors Manager.Start before serving Puts.
	c.LoadCatalog()
	return c
}

func TestCachePutGetHas(t *testing.T) {
	c := newTestCache(t, 10*1024*1024, 0)

	data := []byte("hello usenet segment")
	require.NoError(t, c.Put("msg-001@nntp.test", data))

	assert.True(t, c.Has("msg-001@nntp.test"))
	assert.False(t, c.Has("msg-999@nntp.test"))

	got, ok := c.Get("msg-001@nntp.test")
	require.True(t, ok)
	assert.Equal(t, data, got)

	assert.EqualValues(t, 1, c.ItemCount())
	assert.EqualValues(t, len(data), c.TotalSize())
}

func TestCacheGetMiss(t *testing.T) {
	c := newTestCache(t, 10*1024*1024, 0)

	data, ok := c.Get("nonexistent@msg")
	assert.False(t, ok)
	assert.Nil(t, data)
}

func TestCacheEvictLRU(t *testing.T) {
	// Allow 100 bytes total. Each entry is 10 bytes, so the 95% low-water
	// mark (95 bytes) leaves room for several entries after a sweep.
	const maxBytes = 100
	c := newTestCache(t, maxBytes, 0)

	require.NoError(t, c.Put("old@msg", []byte("0123456789"))) // 10 bytes — oldest
	time.Sleep(5 * time.Millisecond)
	for i := 0; i < 9; i++ {
		require.NoError(t, c.Put(fmt.Sprintf("fill-%d@msg", i), []byte("abcdefghij")))
		time.Sleep(time.Millisecond)
	}

	assert.EqualValues(t, 10, c.ItemCount())
	assert.EqualValues(t, maxBytes, c.TotalSize())

	// Adding one more entry pushes past the budget and evicts the oldest.
	require.NoError(t, c.Put("newest@msg", []byte("ABCDEFGHIJ")))

	assert.False(t, c.Has("old@msg"), "oldest entry should have been evicted")
	assert.True(t, c.Has("newest@msg"), "newest entry should be retained")
	assert.LessOrEqual(t, c.TotalSize(), int64(maxBytes*95/100))
}

func TestCachePutEnforcesMaxSizeWithoutExplicitEvict(t *testing.T) {
	const maxBytes = 100
	c := newTestCache(t, maxBytes, 0)

	// Write well past the budget; no explicit Evict() call anywhere.
	for i := 0; i < 50; i++ {
		require.NoError(t, c.Put(fmt.Sprintf("seg-%d@msg", i), []byte("0123456789")))
		assert.LessOrEqual(t, c.TotalSize(), int64(maxBytes),
			"cache exceeded MaxSizeBytes after Put %d", i)
	}
}

func TestCacheConcurrentPutsRespectMaxSize(t *testing.T) {
	const maxBytes = 1000
	c := newTestCache(t, maxBytes, 0)

	payload := make([]byte, 100)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				assert.NoError(t, c.Put(fmt.Sprintf("w%d-seg-%d@msg", worker, i), payload))
			}
		}(w)
	}
	wg.Wait()

	// A concurrent Put may land while a sweep is in flight, so the final size can
	// sit above the low-water mark, but it must settle within the budget.
	c.Evict()
	assert.LessOrEqual(t, c.TotalSize(), int64(maxBytes))
}

func TestCacheCleanupExpiry(t *testing.T) {
	c := newTestCache(t, 10*1024*1024, 50*time.Millisecond)

	require.NoError(t, c.Put("expires@msg", []byte("data")))
	assert.True(t, c.Has("expires@msg"))

	// Wait for expiry then run cleanup.
	time.Sleep(100 * time.Millisecond)
	c.Cleanup()

	assert.False(t, c.Has("expires@msg"), "entry should have been cleaned up after expiry")
}

func TestCacheSaveCatalogAndReload(t *testing.T) {
	dir := t.TempDir()
	cfg := segcache.Config{
		CachePath:    dir,
		MaxSizeBytes: 10 * 1024 * 1024,
	}

	// Write entries, save catalog, then reload.
	c1, err := segcache.NewSegmentCache(cfg, slog.Default())
	require.NoError(t, err)
	c1.LoadCatalog()
	require.NoError(t, c1.Put("persist@msg", []byte("persistent data")))
	require.NoError(t, c1.SaveCatalog())

	// Load a new cache from the same directory.
	c2, err := segcache.NewSegmentCache(cfg, slog.Default())
	require.NoError(t, err)
	c2.LoadCatalog()

	assert.True(t, c2.Has("persist@msg"), "reloaded cache should contain persisted entry")
	got, ok := c2.Get("persist@msg")
	require.True(t, ok)
	assert.Equal(t, []byte("persistent data"), got)
}

func TestCachePutOverwrite(t *testing.T) {
	c := newTestCache(t, 10*1024*1024, 0)

	require.NoError(t, c.Put("dup@msg", []byte("first")))
	require.NoError(t, c.Put("dup@msg", []byte("second")))

	// Size should reflect overwrite (not accumulate).
	assert.EqualValues(t, len("second"), c.TotalSize())

	got, ok := c.Get("dup@msg")
	require.True(t, ok)
	assert.Equal(t, []byte("second"), got)
}

func TestCacheCatalogSurvivesMissingSegFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := segcache.Config{
		CachePath:    dir,
		MaxSizeBytes: 10 * 1024 * 1024,
	}

	c1, err := segcache.NewSegmentCache(cfg, slog.Default())
	require.NoError(t, err)
	c1.LoadCatalog()
	require.NoError(t, c1.Put("good@msg", []byte("good")))
	require.NoError(t, c1.Put("bad@msg", []byte("will be deleted")))
	require.NoError(t, c1.SaveCatalog())

	// Delete the .seg file for "bad@msg" to simulate corruption.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, de := range entries {
		if filepath.Ext(de.Name()) == ".seg" {
			content, readErr := os.ReadFile(filepath.Join(dir, de.Name()))
			if readErr != nil {
				continue
			}
			if string(content) == "will be deleted" {
				require.NoError(t, os.Remove(filepath.Join(dir, de.Name())))
				break
			}
		}
	}

	// Reload: only "good@msg" should survive.
	c2, err := segcache.NewSegmentCache(cfg, slog.Default())
	require.NoError(t, err)
	c2.LoadCatalog()

	assert.True(t, c2.Has("good@msg"))
	assert.False(t, c2.Has("bad@msg"), "entry with missing seg file should be dropped on reload")
}

