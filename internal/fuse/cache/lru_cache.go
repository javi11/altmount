package cache

import (
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// LRUCache implements the Cache interface using LRU eviction with TTL expiration.
type LRUCache struct {
	statCache     *lru.Cache[string, *statEntry]
	dirCache      *lru.Cache[string, *dirEntry]
	negativeCache *lru.Cache[string, *negativeEntry]

	statTTL     time.Duration
	dirTTL      time.Duration
	negativeTTL time.Duration

	// Statistics
	statHits       atomic.Uint64
	statMisses     atomic.Uint64
	dirHits        atomic.Uint64
	dirMisses      atomic.Uint64
	negativeHits   atomic.Uint64
	negativeMisses atomic.Uint64

	// Mutex for prefix invalidation operations
	mu sync.RWMutex
}

// statEntry holds a cached stat result with expiration time.
type statEntry struct {
	info      os.FileInfo
	expiresAt time.Time
}

// dirEntry holds cached directory entries with expiration time.
type dirEntry struct {
	entries   []DirEntry
	expiresAt time.Time
}

// negativeEntry holds a negative lookup result with expiration time.
type negativeEntry struct {
	expiresAt time.Time
}

// NewLRUCache creates a new LRU-based cache with the given configuration.
func NewLRUCache(cfg Config) (*LRUCache, error) {
	statCache, err := lru.New[string, *statEntry](cfg.StatCacheSize)
	if err != nil {
		return nil, err
	}

	dirCache, err := lru.New[string, *dirEntry](cfg.DirCacheSize)
	if err != nil {
		return nil, err
	}

	negativeCache, err := lru.New[string, *negativeEntry](cfg.NegativeCacheSize)
	if err != nil {
		return nil, err
	}

	return &LRUCache{
		statCache:     statCache,
		dirCache:      dirCache,
		negativeCache: negativeCache,
		statTTL:       cfg.StatTTL,
		dirTTL:        cfg.DirTTL,
		negativeTTL:   cfg.NegativeTTL,
	}, nil
}

// GetStat retrieves a cached stat result.
func (c *LRUCache) GetStat(path string) (os.FileInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.statCache.Get(path)
	if !ok {
		c.statMisses.Add(1)
		return nil, false
	}

	// Check if expired
	if time.Now().After(entry.expiresAt) {
		c.statCache.Remove(path)
		c.statMisses.Add(1)
		return nil, false
	}

	c.statHits.Add(1)
	return entry.info, true
}

// SetStat caches a stat result.
func (c *LRUCache) SetStat(path string, info os.FileInfo) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	c.statCache.Add(path, &statEntry{
		info:      info,
		expiresAt: time.Now().Add(c.statTTL),
	})

	// Remove from negative cache since the file now exists
	c.negativeCache.Remove(path)
}

// GetDirEntries retrieves cached directory entries.
func (c *LRUCache) GetDirEntries(path string) ([]DirEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.dirCache.Get(path)
	if !ok {
		c.dirMisses.Add(1)
		return nil, false
	}

	// Check if expired
	if time.Now().After(entry.expiresAt) {
		c.dirCache.Remove(path)
		c.dirMisses.Add(1)
		return nil, false
	}

	c.dirHits.Add(1)
	// Return a copy to prevent mutation
	result := make([]DirEntry, len(entry.entries))
	copy(result, entry.entries)
	return result, true
}

// SetDirEntries caches directory entries.
func (c *LRUCache) SetDirEntries(path string, entries []DirEntry) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Store a copy to prevent external mutation
	entriesCopy := make([]DirEntry, len(entries))
	copy(entriesCopy, entries)

	c.dirCache.Add(path, &dirEntry{
		entries:   entriesCopy,
		expiresAt: time.Now().Add(c.dirTTL),
	})
}

// IsNegative checks if a path is in the negative cache (known to not exist).
func (c *LRUCache) IsNegative(path string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.negativeCache.Get(path)
	if !ok {
		c.negativeMisses.Add(1)
		return false
	}

	// Check if expired
	if time.Now().After(entry.expiresAt) {
		c.negativeCache.Remove(path)
		c.negativeMisses.Add(1)
		return false
	}

	c.negativeHits.Add(1)
	return true
}

// SetNegative marks a path as non-existent.
func (c *LRUCache) SetNegative(path string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	c.negativeCache.Add(path, &negativeEntry{
		expiresAt: time.Now().Add(c.negativeTTL),
	})
}

// Invalidate removes all cached data for a specific path.
func (c *LRUCache) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.statCache.Remove(path)
	c.dirCache.Remove(path)
	c.negativeCache.Remove(path)
}

// InvalidatePrefix removes all cached data for paths starting with the given prefix.
func (c *LRUCache) InvalidatePrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Ensure prefix ends with / for directory matching
	normalizedPrefix := prefix
	if !strings.HasSuffix(normalizedPrefix, "/") {
		normalizedPrefix += "/"
	}

	// Invalidate stat cache entries with matching prefix
	for _, key := range c.statCache.Keys() {
		if strings.HasPrefix(key, normalizedPrefix) || key == prefix {
			c.statCache.Remove(key)
		}
	}

	// Invalidate dir cache entries with matching prefix
	for _, key := range c.dirCache.Keys() {
		if strings.HasPrefix(key, normalizedPrefix) || key == prefix {
			c.dirCache.Remove(key)
		}
	}

	// Invalidate negative cache entries with matching prefix
	for _, key := range c.negativeCache.Keys() {
		if strings.HasPrefix(key, normalizedPrefix) || key == prefix {
			c.negativeCache.Remove(key)
		}
	}
}

// InvalidateAll clears all caches.
func (c *LRUCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.statCache.Purge()
	c.dirCache.Purge()
	c.negativeCache.Purge()
}

// Stats returns cache statistics.
func (c *LRUCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		StatCacheSize:     c.statCache.Len(),
		StatCacheHits:     c.statHits.Load(),
		StatCacheMisses:   c.statMisses.Load(),
		DirCacheSize:      c.dirCache.Len(),
		DirCacheHits:      c.dirHits.Load(),
		DirCacheMisses:    c.dirMisses.Load(),
		NegativeCacheSize: c.negativeCache.Len(),
		NegativeHits:      c.negativeHits.Load(),
		NegativeMisses:    c.negativeMisses.Load(),
	}
}
