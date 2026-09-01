package filesystem

import (
	"container/list"
	"sync"

	"github.com/javi11/altmount/internal/usenet"
)

// DefaultImportSegmentCacheBytes bounds the default size of an
// ImportSegmentCache. Decoded Usenet segments are ~750 KB each, so 128 MiB
// holds roughly 170 of them — enough to absorb the repeated header/prefix
// reads a single archive-analysis pass performs (rardecode's parallel volume
// reads, 7zip's central-directory ReadAt calls, ISO structure scans) without
// letting an unbounded cache OOM the importer.
const DefaultImportSegmentCacheBytes = 128 * 1024 * 1024

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
	curBytes int64
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

// Get returns the cached bytes for messageID, promoting the entry to
// most-recently-used on hit.
func (c *ImportSegmentCache) Get(messageID string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[messageID]
	if !ok {
		return nil, false
	}
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
		entry := back.Value.(*importSegmentCacheEntry)
		c.order.Remove(back)
		delete(c.items, entry.id)
		c.curBytes -= int64(len(entry.data))
	}

	return nil
}
