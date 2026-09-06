package filesystem

import (
	"container/list"
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/javi11/altmount/internal/usenet"
)

// DefaultImportSegmentCacheBytes bounds the default size of an
// ImportSegmentCache. Decoded Usenet segments are ~750 KB each, so 32 MiB holds
// roughly 43 of them — enough to absorb the repeated header/prefix reads a
// single archive-analysis pass performs (rardecode's parallel volume reads,
// 7zip's central-directory ReadAt calls, ISO structure scans).
//
// The size is set by the worst case, not by one cache. There are five
// construction sites and the nested-RAR pass runs INSIDE the outer pass, so two
// caches are live per import; at max_processor_workers 2 that is four concurrent
// instances. At 128 MiB each that would be 512 MiB, which is far too much to
// spend on the NAS hardware AltMount commonly runs on — especially for a hit
// rate nobody has measured yet. 32 MiB caps the worst case at 128 MiB.
// TestImportSegmentCache_WorstCaseFootprintIsDeliberate pins that budget.
const DefaultImportSegmentCacheBytes = 32 * 1024 * 1024

// ImportSegmentCache is a bounded, in-memory, LRU cache of decoded Usenet
// segment bytes, scoped to a single import (or a single archive-analysis
// pass within one). It implements usenet.SegmentStore.
//
// This is deliberately NOT the disk-backed, process-wide cache in
// internal/nzbfilesystem/segcache: that package is a single global,
// user-configured, catalog-persisting cache with its own background
// maintenance goroutines, sized in gigabytes and meant to survive process
// restarts. Reusing it here would mean either sharing one global instance
// across concurrent imports (defeating the "released when the import
// finishes" requirement and forcing disk I/O — a temp-write plus rename —
// on every segment fetch) or spinning up a full Manager, with its own
// catalog directory and cleanup/flush goroutines, per archive-analysis
// pass. An import-scoped cache has no need for any of that: it lives only
// in memory, is created fresh per pass, and is released simply by dropping
// the reference once that pass returns — a plain bounded LRU is the
// correct-sized tool.
//
// Safe for concurrent use: rardecode's ParallelRead(true) mode reads
// multiple volumes from separate goroutines concurrently.
type ImportSegmentCache struct {
	mu       sync.Mutex
	items    map[string]*list.Element
	order    *list.List // front = most recently used
	maxBytes int64

	// Counters are guarded by mu alongside the map, so a Stats() snapshot is
	// consistent with the entries it describes.
	hits      int64
	misses    int64
	evictions int64
	curBytes  int64
}

type importSegmentCacheEntry struct {
	id   string
	data []byte
}

var _ usenet.SegmentStore = (*ImportSegmentCache)(nil)

// NewImportSegmentCache creates a bounded, in-memory LRU segment cache.
// maxBytes <= 0 falls back to DefaultImportSegmentCacheBytes.
func NewImportSegmentCache(maxBytes int64) *ImportSegmentCache {
	if maxBytes <= 0 {
		maxBytes = DefaultImportSegmentCacheBytes
	}
	return &ImportSegmentCache{
		items:    make(map[string]*list.Element),
		order:    list.New(),
		maxBytes: maxBytes,
	}
}

// ImportSegmentCacheStats is a snapshot of one cache's activity. It exists so
// an operator can tell from a real import whether the cache earns the memory it
// reserves — the hit rate was never measured before shipping, only assumed from
// how archive analysis was believed to read.
type ImportSegmentCacheStats struct {
	Hits      int64
	Misses    int64
	Evictions int64
	// Entries and Bytes describe the cache at the moment of the snapshot.
	Entries int
	Bytes   int64
}

// HitRate is hits as a fraction of all requests, or 0 when nothing was
// requested. Computed here so every caller does not repeat the zero guard.
func (s ImportSegmentCacheStats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}

// Stats snapshots the cache's activity counters and current occupancy.
func (c *ImportSegmentCache) Stats() ImportSegmentCacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ImportSegmentCacheStats{
		Hits:      c.hits,
		Misses:    c.misses,
		Evictions: c.evictions,
		Entries:   len(c.items),
		Bytes:     c.curBytes,
	}
}

// LogStats writes a one-line summary of this cache's effectiveness. Call it
// once, as the analysis pass finishes.
//
// Logged at info rather than debug on purpose: the counters exist so the hit
// rate can be read off a real import, which is the one thing that was never
// measured before the cache shipped. It is one line per analysis pass, not per
// segment, so the volume is negligible. A pass that never touched the cache
// logs nothing.
func (c *ImportSegmentCache) LogStats(ctx context.Context, log *slog.Logger, pass string) {
	if c == nil {
		return
	}
	s := c.Stats()
	if s.Hits+s.Misses == 0 {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	log.InfoContext(ctx, "Import segment cache",
		"pass", pass,
		"hit_rate", fmt.Sprintf("%.2f", s.HitRate()),
		"hits", s.Hits,
		"misses", s.Misses,
		"evictions", s.Evictions,
		"entries", s.Entries,
		"bytes", s.Bytes,
	)
}

// Get returns the cached bytes for messageID, promoting the entry to
// most-recently-used on hit.
func (c *ImportSegmentCache) Get(messageID string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[messageID]
	if !ok {
		c.misses++
		return nil, false
	}
	c.hits++
	c.order.MoveToFront(el)
	entry := el.Value.(*importSegmentCacheEntry)
	return entry.data, true
}

// Put stores data for messageID, evicting least-recently-used entries as
// needed to stay within maxBytes.
func (c *ImportSegmentCache) Put(messageID string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[messageID]; ok {
		entry := el.Value.(*importSegmentCacheEntry)
		c.curBytes -= int64(len(entry.data))
		entry.data = data
		c.curBytes += int64(len(data))
		c.order.MoveToFront(el)
	} else {
		entry := &importSegmentCacheEntry{id: messageID, data: data}
		c.items[messageID] = c.order.PushFront(entry)
		c.curBytes += int64(len(data))
	}

	for c.curBytes > c.maxBytes {
		back := c.order.Back()
		if back == nil {
			break
		}
		c.evictions++
		entry := back.Value.(*importSegmentCacheEntry)
		c.order.Remove(back)
		delete(c.items, entry.id)
		c.curBytes -= int64(len(entry.data))
	}

	return nil
}
