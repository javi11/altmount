// Package cache provides metadata caching for the FUSE filesystem to reduce
// queries to the underlying metadata service.
package cache

import (
	"os"
	"time"
)

// Cache defines the interface for FUSE metadata caching.
// It provides separate caches for stat results, directory listings,
// and negative lookups (file not found).
type Cache interface {
	// Stat operations
	GetStat(path string) (os.FileInfo, bool)
	SetStat(path string, info os.FileInfo)

	// Directory listing operations
	GetDirEntries(path string) ([]DirEntry, bool)
	SetDirEntries(path string, entries []DirEntry)

	// Negative cache (file not found)
	IsNegative(path string) bool
	SetNegative(path string)

	// Invalidation
	Invalidate(path string)
	InvalidatePrefix(prefix string)
	InvalidateAll()

	// Statistics
	Stats() CacheStats
}

// DirEntry represents a directory entry for caching.
type DirEntry struct {
	Name  string
	IsDir bool
	Mode  os.FileMode
}

// CacheStats provides cache statistics for monitoring.
type CacheStats struct {
	StatCacheSize     int
	StatCacheHits     uint64
	StatCacheMisses   uint64
	DirCacheSize      int
	DirCacheHits      uint64
	DirCacheMisses    uint64
	NegativeCacheSize int
	NegativeHits      uint64
	NegativeMisses    uint64
}

// Config holds configuration for the cache.
type Config struct {
	StatCacheSize     int
	DirCacheSize      int
	NegativeCacheSize int
	StatTTL           time.Duration
	DirTTL            time.Duration
	NegativeTTL       time.Duration
}

// DefaultConfig returns a cache configuration with sensible defaults.
// Memory budget: ~2.2 MB total
// - Stat cache: ~1.5 MB (10K entries × ~150 bytes)
// - Dir cache: ~500 KB (1K entries × ~500 bytes avg)
// - Negative cache: ~200 KB (5K entries × ~40 bytes)
func DefaultConfig() Config {
	return Config{
		StatCacheSize:     10000,
		DirCacheSize:      1000,
		NegativeCacheSize: 5000,
		StatTTL:           30 * time.Second,
		DirTTL:            60 * time.Second,
		NegativeTTL:       10 * time.Second,
	}
}
