package vfs

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const metadataSuffix = ".meta"

// CacheConfig holds disk cache configuration.
type CacheConfig struct {
	CachePath      string
	MaxSizeBytes   int64
	ExpiryDuration time.Duration
	ChunkSize      int64
}

// itemMeta is persisted as JSON alongside each data file.
type itemMeta struct {
	Path       string  `json:"path"`
	FileSize   int64   `json:"file_size"`
	Ranges     []Range `json:"ranges"`
	LastAccess int64   `json:"last_access"`
	Created    int64   `json:"created"`
}

// CacheItem represents a single cached file on disk.
type CacheItem struct {
	mu        sync.RWMutex
	meta      itemMeta
	ranges    *Ranges
	dataPath  string
	metaPath  string
	dataFile  *os.File
	openCount int
	dirty     bool
	chunkSize int64
}

// Cache manages disk-cached files.
type Cache struct {
	mu     sync.Mutex
	items  map[string]*CacheItem
	config CacheConfig
	logger *slog.Logger
}

// NewCache creates a new disk cache.
func NewCache(cfg CacheConfig, logger *slog.Logger) (*Cache, error) {
	if err := os.MkdirAll(cfg.CachePath, 0755); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}

	c := &Cache{
		items:  make(map[string]*CacheItem),
		config: cfg,
		logger: logger,
	}

	// Load existing cached items from disk
	c.loadExisting()

	return c, nil
}

// GetOrCreate returns an existing cache item or creates a new one.
func (c *Cache) GetOrCreate(path string, fileSize int64) *CacheItem {
	c.mu.Lock()
	defer c.mu.Unlock()

	if item, ok := c.items[path]; ok {
		item.mu.Lock()
		item.meta.LastAccess = time.Now().Unix()
		item.dirty = true
		item.mu.Unlock()
		return item
	}

	safeName := sanitizePath(path)
	dataPath := filepath.Join(c.config.CachePath, safeName)
	metaPath := dataPath + metadataSuffix

	item := &CacheItem{
		meta: itemMeta{
			Path:       path,
			FileSize:   fileSize,
			LastAccess: time.Now().Unix(),
			Created:    time.Now().Unix(),
		},
		ranges:    NewRanges(),
		dataPath:  dataPath,
		metaPath:  metaPath,
		chunkSize: c.config.ChunkSize,
		dirty:     true,
	}

	c.items[path] = item
	return item
}

// Remove removes a cache item and deletes its files.
func (c *Cache) Remove(path string) {
	c.mu.Lock()
	item, ok := c.items[path]
	if ok {
		delete(c.items, path)
	}
	c.mu.Unlock()

	if ok {
		item.mu.Lock()
		if item.dataFile != nil {
			item.dataFile.Close()
			item.dataFile = nil
		}
		item.mu.Unlock()

		os.Remove(item.dataPath)
		os.Remove(item.metaPath)
	}
}

// Cleanup removes expired and oversized cache entries.
// Active files (openCount > 0) are never evicted.
func (c *Cache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().Unix()
	expiry := int64(c.config.ExpiryDuration.Seconds())

	// Phase 1: Remove expired items (only if not actively open)
	for path, item := range c.items {
		item.mu.RLock()
		isOpen := item.openCount > 0
		lastAccess := item.meta.LastAccess
		item.mu.RUnlock()

		if !isOpen && expiry > 0 && (now-lastAccess) > expiry {
			c.removeItemLocked(path, item)
		}
	}

	// Phase 2: Size-based eviction (LRU, skip active files)
	if c.config.MaxSizeBytes <= 0 {
		return
	}

	totalSize := c.totalSizeLocked()
	if totalSize <= c.config.MaxSizeBytes {
		return
	}

	// Sort by last access time (oldest first)
	type entry struct {
		path string
		item *CacheItem
		time int64
	}

	var candidates []entry
	for path, item := range c.items {
		item.mu.RLock()
		isOpen := item.openCount > 0
		lastAccess := item.meta.LastAccess
		item.mu.RUnlock()

		if !isOpen {
			candidates = append(candidates, entry{path, item, lastAccess})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].time < candidates[j].time
	})

	for _, e := range candidates {
		if totalSize <= c.config.MaxSizeBytes {
			break
		}

		e.item.mu.RLock()
		itemSize := e.item.ranges.Size()
		e.item.mu.RUnlock()

		c.removeItemLocked(e.path, e.item)
		totalSize -= itemSize
	}
}

// FlushMetadata persists metadata for all dirty items.
func (c *Cache) FlushMetadata() {
	c.mu.Lock()
	items := make(map[string]*CacheItem, len(c.items))
	maps.Copy(items, c.items)
	c.mu.Unlock()

	for _, item := range items {
		item.mu.Lock()
		if item.dirty {
			item.meta.Ranges = item.ranges.Items()
			if err := item.writeMeta(); err != nil {
				c.logger.Warn("Failed to flush cache metadata", "path", item.meta.Path, "error", err)
			}
			item.dirty = false
		}
		item.mu.Unlock()
	}
}

// TotalSize returns the total cached data size in bytes.
func (c *Cache) TotalSize() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalSizeLocked()
}

// ItemCount returns the number of cached items.
func (c *Cache) ItemCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *Cache) totalSizeLocked() int64 {
	var total int64
	for _, item := range c.items {
		item.mu.RLock()
		total += item.ranges.Size()
		item.mu.RUnlock()
	}
	return total
}

func (c *Cache) removeItemLocked(path string, item *CacheItem) {
	item.mu.Lock()
	if item.dataFile != nil {
		item.dataFile.Close()
		item.dataFile = nil
	}
	item.mu.Unlock()

	os.Remove(item.dataPath)
	os.Remove(item.metaPath)
	delete(c.items, path)
}

func (c *Cache) loadExisting() {
	entries, err := os.ReadDir(c.config.CachePath)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != metadataSuffix {
			continue
		}

		metaPath := filepath.Join(c.config.CachePath, entry.Name())
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}

		var meta itemMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}

		dataPath := metaPath[:len(metaPath)-len(metadataSuffix)]
		ranges := NewRanges()
		ranges.FromItems(meta.Ranges)

		item := &CacheItem{
			meta:      meta,
			ranges:    ranges,
			dataPath:  dataPath,
			metaPath:  metaPath,
			chunkSize: c.config.ChunkSize,
		}

		c.items[meta.Path] = item
	}

	if len(c.items) > 0 {
		c.logger.Info("Loaded existing VFS cache items", "count", len(c.items))
	}
}

// CacheItem methods

// Open increments the open count and ensures the data file is open.
func (ci *CacheItem) Open() error {
	ci.mu.Lock()
	defer ci.mu.Unlock()

	ci.openCount++
	ci.meta.LastAccess = time.Now().Unix()
	ci.dirty = true

	if ci.dataFile == nil {
		f, err := os.OpenFile(ci.dataPath, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			ci.openCount--
			return fmt.Errorf("open cache data file: %w", err)
		}
		ci.dataFile = f
	}

	return nil
}

// Close decrements the open count and closes the data file when no longer needed.
func (ci *CacheItem) Close() {
	ci.mu.Lock()
	defer ci.mu.Unlock()

	if ci.openCount > 0 {
		ci.openCount--
	}

	if ci.openCount == 0 && ci.dataFile != nil {
		ci.dataFile.Close()
		ci.dataFile = nil
	}
}

// ReadAt reads cached data at the given offset. Returns the number of bytes read
// and whether the data was fully present in cache.
func (ci *CacheItem) ReadAt(p []byte, off int64) (int, bool) {
	ci.mu.RLock()
	defer ci.mu.RUnlock()

	end := off + int64(len(p))
	if !ci.ranges.Present(off, end) {
		return 0, false
	}

	if ci.dataFile == nil {
		return 0, false
	}

	n, err := ci.dataFile.ReadAt(p, off)
	if err != nil {
		return n, false
	}

	return n, true
}

// WriteAt writes data to the cache at the given offset and marks the range as present.
func (ci *CacheItem) WriteAt(p []byte, off int64) (int, error) {
	ci.mu.Lock()
	defer ci.mu.Unlock()

	if ci.dataFile == nil {
		return 0, fmt.Errorf("cache data file not open")
	}

	// Ensure the sparse file is large enough
	n, err := ci.dataFile.WriteAt(p, off)
	if err != nil {
		return n, err
	}

	ci.ranges.Insert(off, off+int64(n))
	ci.meta.LastAccess = time.Now().Unix()
	ci.dirty = true

	return n, nil
}

// HasRange checks if a byte range is fully present in cache.
func (ci *CacheItem) HasRange(start, end int64) bool {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	return ci.ranges.Present(start, end)
}

// MissingRanges returns byte ranges within [start, end) not yet cached.
func (ci *CacheItem) MissingRanges(start, end int64) []Range {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	return ci.ranges.FindMissing(start, end)
}

// CachedSize returns the total bytes cached for this item.
func (ci *CacheItem) CachedSize() int64 {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	return ci.ranges.Size()
}

// FileSize returns the full file size.
func (ci *CacheItem) FileSize() int64 {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	return ci.meta.FileSize
}

func (ci *CacheItem) writeMeta() error {
	data, err := json.Marshal(&ci.meta)
	if err != nil {
		return err
	}
	return os.WriteFile(ci.metaPath, data, 0644)
}

// sanitizePath converts a filesystem path to a safe filename for the cache.
func sanitizePath(path string) string {
	// Use a simple hash-like encoding to avoid path separator issues
	safe := make([]byte, 0, len(path))
	for i := 0; i < len(path); i++ {
		c := path[i]
		switch c {
		case '/', '\\':
			safe = append(safe, '_')
		case '_':
			safe = append(safe, '_', '_')
		default:
			safe = append(safe, c)
		}
	}
	return string(safe)
}
