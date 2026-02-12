package vfs

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCacheConfig(t *testing.T) CacheConfig {
	t.Helper()
	dir := t.TempDir()
	return CacheConfig{
		CachePath:      dir,
		MaxSizeBytes:   1024 * 1024, // 1MB
		ExpiryDuration: 1 * time.Hour,
		ChunkSize:      64,
	}
}

func TestCache_GetOrCreate(t *testing.T) {
	cfg := testCacheConfig(t)
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	item := c.GetOrCreate("/test/file.mkv", 1000)
	assert.NotNil(t, item)

	// Same path should return same item
	item2 := c.GetOrCreate("/test/file.mkv", 1000)
	assert.Equal(t, item, item2)
	assert.Equal(t, 1, c.ItemCount())
}

func TestCache_Remove(t *testing.T) {
	cfg := testCacheConfig(t)
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	item := c.GetOrCreate("/test/file.mkv", 1000)
	require.NoError(t, item.Open())

	// Write some data
	data := []byte("hello world")
	_, writeErr := item.WriteAt(data, 0)
	require.NoError(t, writeErr)
	item.Close()

	c.Remove("/test/file.mkv")
	assert.Equal(t, 0, c.ItemCount())

	// Files should be cleaned up
	_, err = os.Stat(item.dataPath)
	assert.True(t, os.IsNotExist(err))
}

func TestCacheItem_ReadWrite(t *testing.T) {
	cfg := testCacheConfig(t)
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	item := c.GetOrCreate("/test/file.mkv", 100)
	require.NoError(t, item.Open())
	defer item.Close()

	// Write data at offset 10
	data := []byte("hello")
	n, writeErr := item.WriteAt(data, 10)
	require.NoError(t, writeErr)
	assert.Equal(t, 5, n)

	// Read it back
	buf := make([]byte, 5)
	n, ok := item.ReadAt(buf, 10)
	assert.True(t, ok)
	assert.Equal(t, 5, n)
	assert.Equal(t, data, buf)

	// Read from uncached range should fail
	n, ok = item.ReadAt(buf, 0)
	assert.False(t, ok)
}

func TestCacheItem_HasRange(t *testing.T) {
	cfg := testCacheConfig(t)
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	item := c.GetOrCreate("/test/file.mkv", 100)
	require.NoError(t, item.Open())
	defer item.Close()

	_, writeErr := item.WriteAt([]byte("hello"), 10)
	require.NoError(t, writeErr)

	assert.True(t, item.HasRange(10, 15))
	assert.False(t, item.HasRange(0, 5))
	assert.False(t, item.HasRange(5, 15)) // partially missing
}

func TestCacheItem_MissingRanges(t *testing.T) {
	cfg := testCacheConfig(t)
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	item := c.GetOrCreate("/test/file.mkv", 100)
	require.NoError(t, item.Open())
	defer item.Close()

	_, writeErr := item.WriteAt([]byte("hello"), 10)
	require.NoError(t, writeErr)

	missing := item.MissingRanges(0, 20)
	assert.Equal(t, []Range{{Start: 0, End: 10}, {Start: 15, End: 20}}, missing)
}

func TestCacheItem_OpenClose_RefCounting(t *testing.T) {
	cfg := testCacheConfig(t)
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	item := c.GetOrCreate("/test/file.mkv", 100)

	// Open twice
	require.NoError(t, item.Open())
	require.NoError(t, item.Open())

	// Write some data
	_, writeErr := item.WriteAt([]byte("test"), 0)
	require.NoError(t, writeErr)

	// First close shouldn't close the file
	item.Close()
	buf := make([]byte, 4)
	_, ok := item.ReadAt(buf, 0)
	assert.True(t, ok) // Still open

	// Second close should close the file
	item.Close()
}

func TestCache_Cleanup_Expiry(t *testing.T) {
	cfg := testCacheConfig(t)
	cfg.ExpiryDuration = 1 * time.Second
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	item := c.GetOrCreate("/test/old.mkv", 100)
	require.NoError(t, item.Open())
	_, writeErr := item.WriteAt([]byte("data"), 0)
	require.NoError(t, writeErr)
	item.Close()

	// Force the last access time to be old enough for expiry
	item.mu.Lock()
	item.meta.LastAccess = time.Now().Add(-2 * time.Second).Unix()
	item.mu.Unlock()

	c.Cleanup()
	assert.Equal(t, 0, c.ItemCount())
}

func TestCache_Cleanup_SkipsActiveFiles(t *testing.T) {
	cfg := testCacheConfig(t)
	cfg.ExpiryDuration = 1 * time.Second
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	item := c.GetOrCreate("/test/active.mkv", 100)
	require.NoError(t, item.Open())
	_, writeErr := item.WriteAt([]byte("data"), 0)
	require.NoError(t, writeErr)
	// Deliberately NOT closing to keep it active

	// Force last access to be old
	item.mu.Lock()
	item.meta.LastAccess = time.Now().Add(-2 * time.Second).Unix()
	item.mu.Unlock()

	c.Cleanup()
	assert.Equal(t, 1, c.ItemCount()) // Should still be present (actively open)

	item.Close()
}

func TestCache_Cleanup_SizeBased(t *testing.T) {
	cfg := testCacheConfig(t)
	cfg.MaxSizeBytes = 10 // Very small to force eviction
	cfg.ExpiryDuration = 0
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	// Create two items exceeding max size
	item1 := c.GetOrCreate("/test/file1.mkv", 100)
	require.NoError(t, item1.Open())
	_, err = item1.WriteAt([]byte("12345678"), 0)
	require.NoError(t, err)
	item1.Close()

	time.Sleep(10 * time.Millisecond)

	item2 := c.GetOrCreate("/test/file2.mkv", 100)
	require.NoError(t, item2.Open())
	_, err = item2.WriteAt([]byte("abcdefgh"), 0)
	require.NoError(t, err)
	item2.Close()

	c.Cleanup()
	// Oldest should be evicted first
	assert.Equal(t, 1, c.ItemCount())
	assert.Equal(t, int64(8), c.TotalSize()) // Only newer item remains
}

func TestCache_FlushMetadata(t *testing.T) {
	cfg := testCacheConfig(t)
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	item := c.GetOrCreate("/test/file.mkv", 100)
	require.NoError(t, item.Open())
	_, writeErr := item.WriteAt([]byte("hello"), 0)
	require.NoError(t, writeErr)
	item.Close()

	c.FlushMetadata()

	// Metadata file should exist
	_, err = os.Stat(item.metaPath)
	assert.NoError(t, err)
}

func TestCache_LoadExisting(t *testing.T) {
	cfg := testCacheConfig(t)
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	item := c.GetOrCreate("/test/file.mkv", 100)
	require.NoError(t, item.Open())
	_, writeErr := item.WriteAt([]byte("hello"), 0)
	require.NoError(t, writeErr)
	item.Close()

	c.FlushMetadata()

	// Create a new cache from the same directory
	c2, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)
	assert.Equal(t, 1, c2.ItemCount())

	// Verify the loaded item works
	item2 := c2.GetOrCreate("/test/file.mkv", 100)
	assert.True(t, item2.HasRange(0, 5))
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/foo/bar.mkv", "_foo_bar.mkv"},
		{"simple.txt", "simple.txt"},
		{"/a/b/c/d.mp4", "_a_b_c_d.mp4"},
		{"file_with_underscore.txt", "file__with__underscore.txt"},
	}

	for _, tt := range tests {
		result := sanitizePath(tt.input)
		assert.Equal(t, tt.expected, result)
		// Verify it's a valid filename (no path separators)
		assert.Equal(t, filepath.Base(result), result)
	}
}
