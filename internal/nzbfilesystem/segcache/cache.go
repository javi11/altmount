// Package segcache provides a segment-aligned disk cache for Usenet file data.
// Unlike the VFS cache which operates on 8MB chunks, the segment cache uses
// Usenet message IDs as cache keys (each ~750KB), matching the actual download
// granularity and enabling cross-file deduplication.
package segcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Config holds segment cache storage settings.
type Config struct {
	CachePath      string
	MaxSizeBytes   int64
	ExpiryDuration time.Duration
}

type cacheEntry struct {
	DataPath   string    `json:"data_path"`
	Size       int64     `json:"size"`
	LastAccess time.Time `json:"last_access"`
	Created    time.Time `json:"created"`
}

// SegmentCache stores decoded segment bytes on disk, keyed by Usenet message ID.
// The in-memory catalog (map[messageID]*cacheEntry) enables O(1) Has() without disk I/O.
// Actual data is stored in per-segment files named by sha256(messageID).
type SegmentCache struct {
	mu        sync.Mutex
	items     map[string]*cacheEntry
	config    Config
	logger    *slog.Logger
	totalSize int64
}

// NewSegmentCache creates a new segment cache, loading any existing catalog.
func NewSegmentCache(cfg Config, logger *slog.Logger) (*SegmentCache, error) {
	if err := os.MkdirAll(cfg.CachePath, 0o755); err != nil {
		return nil, fmt.Errorf("segcache: create cache dir %s: %w", cfg.CachePath, err)
	}

	c := &SegmentCache{
		items:  make(map[string]*cacheEntry),
		config: cfg,
		logger: logger,
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
func (c *SegmentCache) Get(messageID string) ([]byte, bool) {
	c.mu.Lock()
	e, ok := c.items[messageID]
	if !ok {
		c.mu.Unlock()
		return nil, false
	}
	e.LastAccess = time.Now()
	path := e.DataPath
	c.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		c.mu.Lock()
		delete(c.items, messageID)
		c.mu.Unlock()
		return nil, false
	}

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

	return nil
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

	for _, pair := range sorted {
		if c.totalSize <= c.config.MaxSizeBytes {
			break
		}
		_ = os.Remove(pair.e.DataPath)
		c.totalSize -= pair.e.Size
		delete(c.items, pair.id)
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

	for id, e := range c.items {
		if e.LastAccess.Before(deadline) {
			_ = os.Remove(e.DataPath)
			c.totalSize -= e.Size
			delete(c.items, id)
		}
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
func (c *SegmentCache) SaveCatalog() error {
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
