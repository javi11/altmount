package cache

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockFileInfo implements os.FileInfo for testing
type mockFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return m.modTime }
func (m *mockFileInfo) IsDir() bool        { return m.isDir }
func (m *mockFileInfo) Sys() any           { return nil }

func TestNewLRUCache(t *testing.T) {
	cfg := DefaultConfig()
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)
	assert.NotNil(t, cache)
}

func TestStatCache(t *testing.T) {
	cfg := Config{
		StatCacheSize:     100,
		DirCacheSize:      100,
		NegativeCacheSize: 100,
		StatTTL:           1 * time.Hour,
		DirTTL:            1 * time.Hour,
		NegativeTTL:       1 * time.Hour,
	}
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)

	// Test cache miss
	info, ok := cache.GetStat("/test/path")
	assert.False(t, ok)
	assert.Nil(t, info)

	// Test cache set and get
	mockInfo := &mockFileInfo{
		name:    "testfile",
		size:    1024,
		mode:    0644,
		modTime: time.Now(),
		isDir:   false,
	}
	cache.SetStat("/test/path", mockInfo)

	info, ok = cache.GetStat("/test/path")
	assert.True(t, ok)
	assert.Equal(t, "testfile", info.Name())
	assert.Equal(t, int64(1024), info.Size())

	// Check stats
	stats := cache.Stats()
	assert.Equal(t, 1, stats.StatCacheSize)
	assert.Equal(t, uint64(1), stats.StatCacheHits)
	assert.Equal(t, uint64(1), stats.StatCacheMisses)
}

func TestStatCacheTTL(t *testing.T) {
	cfg := Config{
		StatCacheSize:     100,
		DirCacheSize:      100,
		NegativeCacheSize: 100,
		StatTTL:           50 * time.Millisecond, // Very short TTL for testing
		DirTTL:            1 * time.Hour,
		NegativeTTL:       1 * time.Hour,
	}
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)

	mockInfo := &mockFileInfo{name: "testfile", size: 100}
	cache.SetStat("/test/path", mockInfo)

	// Should hit cache
	info, ok := cache.GetStat("/test/path")
	assert.True(t, ok)
	assert.NotNil(t, info)

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Should miss after expiration
	info, ok = cache.GetStat("/test/path")
	assert.False(t, ok)
	assert.Nil(t, info)
}

func TestDirCache(t *testing.T) {
	cfg := DefaultConfig()
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)

	// Test cache miss
	entries, ok := cache.GetDirEntries("/test/dir")
	assert.False(t, ok)
	assert.Nil(t, entries)

	// Test cache set and get
	dirEntries := []DirEntry{
		{Name: "file1.txt", IsDir: false, Mode: 0644},
		{Name: "subdir", IsDir: true, Mode: 0755},
	}
	cache.SetDirEntries("/test/dir", dirEntries)

	entries, ok = cache.GetDirEntries("/test/dir")
	assert.True(t, ok)
	assert.Len(t, entries, 2)
	assert.Equal(t, "file1.txt", entries[0].Name)
	assert.Equal(t, "subdir", entries[1].Name)
	assert.True(t, entries[1].IsDir)

	// Check stats
	stats := cache.Stats()
	assert.Equal(t, 1, stats.DirCacheSize)
	assert.Equal(t, uint64(1), stats.DirCacheHits)
	assert.Equal(t, uint64(1), stats.DirCacheMisses)
}

func TestDirCacheImmutability(t *testing.T) {
	cfg := DefaultConfig()
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)

	dirEntries := []DirEntry{
		{Name: "file1.txt", IsDir: false},
	}
	cache.SetDirEntries("/test/dir", dirEntries)

	// Modify original slice
	dirEntries[0].Name = "modified.txt"

	// Cached copy should be unchanged
	entries, ok := cache.GetDirEntries("/test/dir")
	assert.True(t, ok)
	assert.Equal(t, "file1.txt", entries[0].Name)

	// Modify returned slice
	entries[0].Name = "also_modified.txt"

	// Fetch again - should still be original
	entries2, ok := cache.GetDirEntries("/test/dir")
	assert.True(t, ok)
	assert.Equal(t, "file1.txt", entries2[0].Name)
}

func TestNegativeCache(t *testing.T) {
	cfg := DefaultConfig()
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)

	// Test cache miss
	assert.False(t, cache.IsNegative("/test/nonexistent"))

	// Set negative entry
	cache.SetNegative("/test/nonexistent")

	// Test cache hit
	assert.True(t, cache.IsNegative("/test/nonexistent"))

	// Check stats
	stats := cache.Stats()
	assert.Equal(t, 1, stats.NegativeCacheSize)
	assert.Equal(t, uint64(1), stats.NegativeHits)
	assert.Equal(t, uint64(1), stats.NegativeMisses)
}

func TestNegativeCacheRemovedOnStatSet(t *testing.T) {
	cfg := DefaultConfig()
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)

	// Mark path as negative
	cache.SetNegative("/test/path")
	assert.True(t, cache.IsNegative("/test/path"))

	// Now set stat for same path (file now exists)
	mockInfo := &mockFileInfo{name: "path"}
	cache.SetStat("/test/path", mockInfo)

	// Should no longer be in negative cache
	assert.False(t, cache.IsNegative("/test/path"))

	// Should be in stat cache
	info, ok := cache.GetStat("/test/path")
	assert.True(t, ok)
	assert.NotNil(t, info)
}

func TestInvalidate(t *testing.T) {
	cfg := DefaultConfig()
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)

	// Set all cache types
	cache.SetStat("/test/path", &mockFileInfo{name: "test"})
	cache.SetDirEntries("/test/path", []DirEntry{{Name: "entry"}})
	cache.SetNegative("/test/path")

	// Verify they're set
	_, ok := cache.GetStat("/test/path")
	assert.True(t, ok)
	_, ok = cache.GetDirEntries("/test/path")
	assert.True(t, ok)
	assert.True(t, cache.IsNegative("/test/path"))

	// Invalidate
	cache.Invalidate("/test/path")

	// All should be gone
	_, ok = cache.GetStat("/test/path")
	assert.False(t, ok)
	_, ok = cache.GetDirEntries("/test/path")
	assert.False(t, ok)
	assert.False(t, cache.IsNegative("/test/path"))
}

func TestInvalidatePrefix(t *testing.T) {
	cfg := DefaultConfig()
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)

	// Set entries with various paths
	cache.SetStat("/movies/action/movie1.mkv", &mockFileInfo{name: "movie1.mkv"})
	cache.SetStat("/movies/action/movie2.mkv", &mockFileInfo{name: "movie2.mkv"})
	cache.SetStat("/movies/comedy/movie3.mkv", &mockFileInfo{name: "movie3.mkv"})
	cache.SetStat("/tv/show1/ep1.mkv", &mockFileInfo{name: "ep1.mkv"})
	cache.SetDirEntries("/movies/action", []DirEntry{{Name: "movie1.mkv"}})

	// Invalidate /movies/action prefix
	cache.InvalidatePrefix("/movies/action")

	// Action movies should be gone
	_, ok := cache.GetStat("/movies/action/movie1.mkv")
	assert.False(t, ok)
	_, ok = cache.GetStat("/movies/action/movie2.mkv")
	assert.False(t, ok)
	_, ok = cache.GetDirEntries("/movies/action")
	assert.False(t, ok)

	// Comedy movie should still be there
	_, ok = cache.GetStat("/movies/comedy/movie3.mkv")
	assert.True(t, ok)

	// TV show should still be there
	_, ok = cache.GetStat("/tv/show1/ep1.mkv")
	assert.True(t, ok)
}

func TestInvalidatePrefixExact(t *testing.T) {
	cfg := DefaultConfig()
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)

	// Set entry for exact path
	cache.SetStat("/movies", &mockFileInfo{name: "movies", isDir: true})
	cache.SetStat("/movies/action", &mockFileInfo{name: "action", isDir: true})

	// Invalidate prefix should also remove exact match
	cache.InvalidatePrefix("/movies")

	_, ok := cache.GetStat("/movies")
	assert.False(t, ok)
	_, ok = cache.GetStat("/movies/action")
	assert.False(t, ok)
}

func TestInvalidateAll(t *testing.T) {
	cfg := DefaultConfig()
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)

	// Set various entries
	cache.SetStat("/path1", &mockFileInfo{name: "test1"})
	cache.SetStat("/path2", &mockFileInfo{name: "test2"})
	cache.SetDirEntries("/dir1", []DirEntry{{Name: "entry"}})
	cache.SetNegative("/missing")

	// Verify they're set
	stats := cache.Stats()
	assert.Equal(t, 2, stats.StatCacheSize)
	assert.Equal(t, 1, stats.DirCacheSize)
	assert.Equal(t, 1, stats.NegativeCacheSize)

	// Invalidate all
	cache.InvalidateAll()

	// All should be gone
	stats = cache.Stats()
	assert.Equal(t, 0, stats.StatCacheSize)
	assert.Equal(t, 0, stats.DirCacheSize)
	assert.Equal(t, 0, stats.NegativeCacheSize)
}

func TestLRUEviction(t *testing.T) {
	cfg := Config{
		StatCacheSize:     2, // Very small for testing
		DirCacheSize:      100,
		NegativeCacheSize: 100,
		StatTTL:           1 * time.Hour,
		DirTTL:            1 * time.Hour,
		NegativeTTL:       1 * time.Hour,
	}
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)

	// Add 3 items (exceeds capacity of 2)
	cache.SetStat("/path1", &mockFileInfo{name: "file1"})
	cache.SetStat("/path2", &mockFileInfo{name: "file2"})
	cache.SetStat("/path3", &mockFileInfo{name: "file3"})

	// First item should be evicted
	_, ok := cache.GetStat("/path1")
	assert.False(t, ok)

	// Recent items should still exist
	_, ok = cache.GetStat("/path2")
	assert.True(t, ok)
	_, ok = cache.GetStat("/path3")
	assert.True(t, ok)
}

func TestConcurrency(t *testing.T) {
	cfg := DefaultConfig()
	cache, err := NewLRUCache(cfg)
	require.NoError(t, err)

	done := make(chan bool)
	iterations := 100

	// Writer goroutine
	go func() {
		for i := 0; i < iterations; i++ {
			cache.SetStat("/test/path", &mockFileInfo{name: "test"})
			cache.SetDirEntries("/test/dir", []DirEntry{{Name: "entry"}})
			cache.SetNegative("/test/missing")
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < iterations; i++ {
			cache.GetStat("/test/path")
			cache.GetDirEntries("/test/dir")
			cache.IsNegative("/test/missing")
		}
		done <- true
	}()

	// Invalidator goroutine
	go func() {
		for i := 0; i < iterations; i++ {
			cache.Invalidate("/test/path")
			cache.InvalidatePrefix("/test")
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// Should complete without race conditions or panics
	_ = cache.Stats()
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 10000, cfg.StatCacheSize)
	assert.Equal(t, 1000, cfg.DirCacheSize)
	assert.Equal(t, 5000, cfg.NegativeCacheSize)
	assert.Equal(t, 30*time.Second, cfg.StatTTL)
	assert.Equal(t, 60*time.Second, cfg.DirTTL)
	assert.Equal(t, 10*time.Second, cfg.NegativeTTL)
}
