package segcache_test

import (
	"log/slog"
	"os"
	"path/filepath"
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
	// Allow only 20 bytes total. Each entry is 10 bytes.
	c := newTestCache(t, 20, 0)

	require.NoError(t, c.Put("old@msg", []byte("0123456789"))) // 10 bytes — oldest
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, c.Put("new@msg", []byte("abcdefghij"))) // 10 bytes

	assert.EqualValues(t, 2, c.ItemCount())

	// Adding a third entry should evict the oldest.
	require.NoError(t, c.Put("newest@msg", []byte("ABCDEFGHIJ"))) // 10 bytes
	c.Evict()

	assert.EqualValues(t, 2, c.ItemCount())
	assert.False(t, c.Has("old@msg"), "oldest entry should have been evicted")
	assert.True(t, c.Has("new@msg"))
	assert.True(t, c.Has("newest@msg"))
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
	require.NoError(t, c1.Put("persist@msg", []byte("persistent data")))
	require.NoError(t, c1.SaveCatalog())

	// Load a new cache from the same directory.
	c2, err := segcache.NewSegmentCache(cfg, slog.Default())
	require.NoError(t, err)

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

	assert.True(t, c2.Has("good@msg"))
	assert.False(t, c2.Has("bad@msg"), "entry with missing seg file should be dropped on reload")
}
