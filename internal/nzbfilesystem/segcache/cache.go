// Package segcache provides a segment-aligned disk cache for Usenet file data.
// Unlike the VFS cache which operates on 8MB chunks, the segment cache uses
// Usenet message IDs as cache keys (each ~750KB), matching the actual download
// granularity and enabling cross-file deduplication.
package segcache

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds segment cache storage settings.
type Config struct {
	CachePath        string
	MaxSizeBytes     int64
	ExpiryDuration   time.Duration
	HotCacheMaxBytes int64 // in-memory LRU cap; 0 = disabled
}

type cacheEntry struct {
	DataPath   string    `json:"data_path"`
	Size       int64     `json:"size"`
	LastAccess time.Time `json:"last_access"`
	Created    time.Time `json:"created"`
}

// hotEntry is the value stored in the hot LRU list.
type hotEntry struct {
	id   string
	data []byte
}

// SegmentCache stores decoded segment bytes on disk, keyed by Usenet message ID.
// The in-memory catalog (map[messageID]*cacheEntry) enables O(1) Has() without disk I/O.
// Actual data is stored in per-segment files named by sha256(messageID).
//
// An optional in-memory LRU hot cache avoids repeated os.ReadFile allocations for
// recently-written or recently-read segments (config.HotCacheMaxBytes > 0).
type SegmentCache struct {
	mu        sync.Mutex
	items     map[string]*cacheEntry
	config    Config
	logger    *slog.Logger
	totalSize int64
	dirty     atomic.Bool

	// Hot LRU cache: recently-accessed segment data kept in memory.
	// hotItems maps messageID → list.Element; hotList is front=LRU, back=MRU.
	// All hot cache fields are protected by mu.
	hotItems map[string]*list.Element
	hotList  *list.List
	hotSize  int64
}

// NewSegmentCache creates a new segment cache, loading any existing catalog.
func NewSegmentCache(cfg Config, logger *slog.Logger) (*SegmentCache, error) {
	if err := os.MkdirAll(cfg.CachePath, 0o755); err != nil {
		return nil, fmt.Errorf("segcache: create cache dir %s: %w", cfg.CachePath, err)
	}

	c := &SegmentCache{
		items:    make(map[string]*cacheEntry),
		config:   cfg,
		logger:   logger,
		hotItems: make(map[string]*list.Element),
		hotList:  list.New(),
	}
	c.loadCatalog()

	return c, nil
}

// Has reports whether the segment is present in the cache (no disk I/O).
func (c *SegmentCache) Has(messageID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.items[messageID]
	return ok
}

// Get returns the decoded segment bytes. Returns (nil, false) on miss.
// Checks the in-memory hot cache before falling back to disk.
func (c *SegmentCache) Get(messageID string) ([]byte, bool) {
	c.mu.Lock()
	e, ok := c.items[messageID]
	if !ok {
		c.mu.Unlock()
		return nil, false
	}
	if time.Since(e.LastAccess) > 60*time.Second {
		e.LastAccess = time.Now()
		c.dirty.Store(true)
	}

	// Hot cache hit: return data without disk I/O.
	if el, hot := c.hotItems[messageID]; hot {
		c.hotList.MoveToBack(el)
		data := el.Value.(*hotEntry).data
		c.mu.Unlock()
		return data, true
	}

	path := e.DataPath
	c.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		c.mu.Lock()
		delete(c.items, messageID)
		c.mu.Unlock()
		return nil, false
	}

	c.addToHotCache(messageID, data)
	return data, true
}

// Put stores segment bytes atomically (temp-write + rename).
func (c *SegmentCache) Put(messageID string, data []byte) error {
	h := sha256.Sum256([]byte(messageID))
	filename := hex.EncodeToString(h[:]) + ".seg"
	dataPath := filepath.Join(c.config.CachePath, filename)

	tmpPath := dataPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("segcache: write segment %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, dataPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("segcache: rename segment to %s: %w", dataPath, err)
	}

	now := time.Now()
	e := &cacheEntry{
		DataPath:   dataPath,
		Size:       int64(len(data)),
		LastAccess: now,
		Created:    now,
	}

	c.mu.Lock()
	if old, exists := c.items[messageID]; exists {
		c.totalSize -= old.Size
	}
	c.items[messageID] = e
	c.totalSize += e.Size
	c.mu.Unlock()

	c.dirty.Store(true)
	c.addToHotCache(messageID, data)

	return nil
}

// addToHotCache stores data in the in-memory LRU, evicting least-recently-used entries as
// needed to stay within config.HotCacheMaxBytes. No-op when hot cache is disabled.
func (c *SegmentCache) addToHotCache(id string, data []byte) {
	if c.config.HotCacheMaxBytes <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// If already present, just promote to MRU position.
	if el, exists := c.hotItems[id]; exists {
		c.hotList.MoveToBack(el)
		return
	}

	entry := &hotEntry{id: id, data: data}
	el := c.hotList.PushBack(entry)
	c.hotItems[id] = el
	c.hotSize += int64(len(data))

	// Evict LRU entries until within the size limit.
	for c.hotSize > c.config.HotCacheMaxBytes {
		front := c.hotList.Front()
		if front == nil {
			break
		}
		evicted := c.hotList.Remove(front).(*hotEntry)
		delete(c.hotItems, evicted.id)
		c.hotSize -= int64(len(evicted.data))
	}
}

// Evict removes the oldest entries (by LastAccess) until total size is within MaxSizeBytes.
func (c *SegmentCache) Evict() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.totalSize <= c.config.MaxSizeBytes {
		return
	}

	type kv struct {
		id string
		e  *cacheEntry
	}

	sorted := make([]kv, 0, len(c.items))
	for id, e := range c.items {
		sorted = append(sorted, kv{id, e})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].e.LastAccess.Before(sorted[j].e.LastAccess)
	})

	removed := false
	for _, pair := range sorted {
		if c.totalSize <= c.config.MaxSizeBytes {
			break
		}
		_ = os.Remove(pair.e.DataPath)
		c.totalSize -= pair.e.Size
		delete(c.items, pair.id)
		removed = true
	}
	if removed {
		c.dirty.Store(true)
	}
}

// Cleanup removes entries that have not been accessed within ExpiryDuration.
func (c *SegmentCache) Cleanup() {
	if c.config.ExpiryDuration <= 0 {
		return
	}

	deadline := time.Now().Add(-c.config.ExpiryDuration)

	c.mu.Lock()
	defer c.mu.Unlock()

	removed := false
	for id, e := range c.items {
		if e.LastAccess.Before(deadline) {
			_ = os.Remove(e.DataPath)
			c.totalSize -= e.Size
			delete(c.items, id)
			removed = true
		}
	}
	if removed {
		c.dirty.Store(true)
	}
}

// TotalSize returns the total bytes occupied by cached segments.
func (c *SegmentCache) TotalSize() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalSize
}

// ItemCount returns the number of cached segments.
func (c *SegmentCache) ItemCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// SaveCatalog flushes the in-memory catalog to disk (catalog.json) atomically.
// It is a no-op when the catalog has not changed since the last flush.
func (c *SegmentCache) SaveCatalog() error {
	if !c.dirty.Load() {
		return nil
	}

	c.mu.Lock()
	snapshot := make(map[string]*cacheEntry, len(c.items))
	for k, v := range c.items {
		cp := *v
		snapshot[k] = &cp
	}
	c.mu.Unlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("segcache: marshal catalog: %w", err)
	}

	catalogPath := filepath.Join(c.config.CachePath, "catalog.json")
	tmpPath := catalogPath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("segcache: write catalog: %w", err)
	}

	if err := os.Rename(tmpPath, catalogPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("segcache: rename catalog: %w", err)
	}

	c.dirty.Store(false)

	return nil
}

func (c *SegmentCache) loadCatalog() {
	catalogPath := filepath.Join(c.config.CachePath, "catalog.json")

	data, err := os.ReadFile(catalogPath)
	if err != nil {
		// No existing catalog; start fresh.
		return
	}

	var items map[string]*cacheEntry
	if err := json.Unmarshal(data, &items); err != nil {
		c.logger.Warn("segcache: corrupt catalog, starting fresh", "error", err)
		return
	}

	var totalSize int64
	valid := make(map[string]*cacheEntry, len(items))

	for id, e := range items {
		if _, statErr := os.Stat(e.DataPath); statErr == nil {
			valid[id] = e
			totalSize += e.Size
		}
	}

	c.items = valid
	c.totalSize = totalSize

	c.logger.Info("segcache: catalog loaded", "items", len(valid), "total_bytes", totalSize)
}
