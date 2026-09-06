package segcache

import (
	"container/list"
	"hash/fnv"
	"sync"
	"sync/atomic"
)

// MemoryCache keeps recently decoded articles in memory, keyed by message-ID
// and bounded by bytes. It sits in front of the disk cache so a probe
// followed by playback, or a second handle on the same title, is served
// without a round trip.
//
// Lookups take only a read lock on their shard and mark the entry touched;
// recency is applied lazily by the eviction sweep (CLOCK, second chance), so
// a single stream driving thousands of lookups a second never contends on
// the recency list. Lock order is orderMu, then a shard mutex, never the
// reverse: two puts of one id must not interleave and leak bytes against
// the budget.
type MemoryCache struct {
	shards [memShards]memShard

	orderMu  sync.Mutex
	order    *list.List // front = most recently inserted or second-chanced
	size     int64
	capacity int64
}

const memShards = 16

// memEntryOverhead is the fixed bookkeeping charged per entry so thousands
// of small articles are not invisible to the budget.
const memEntryOverhead = 128

type memShard struct {
	mu sync.RWMutex
	m  map[string]*memEntry
}

type memEntry struct {
	id      string
	data    []byte
	touched atomic.Bool
	elem    *list.Element
}

func NewMemoryCache(maxBytes int64) *MemoryCache {
	m := &MemoryCache{order: list.New(), capacity: max(maxBytes, 0)}
	for i := range m.shards {
		m.shards[i].m = make(map[string]*memEntry)
	}
	return m
}

func (m *MemoryCache) shard(id string) *memShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return &m.shards[h.Sum32()%memShards]
}

func entryCost(data []byte) int64 { return int64(cap(data)) + memEntryOverhead }

// Get returns the cached article and marks it recently used.
func (m *MemoryCache) Get(id string) ([]byte, bool) {
	if m == nil {
		return nil, false
	}
	s := m.shard(id)
	s.mu.RLock()
	e, ok := s.m[id]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	e.touched.Store(true)
	return e.data, true
}

// Put stores an article, evicting the least recently used entries as needed.
// The budget charges capacity, not length, since that is the memory held.
// An article larger than the whole budget is not cached rather than
// thrashing everything else out.
func (m *MemoryCache) Put(id string, data []byte) {
	if m == nil || len(data) == 0 {
		return
	}
	cost := entryCost(data)
	m.orderMu.Lock()
	defer m.orderMu.Unlock()
	if m.capacity <= 0 || cost > m.capacity {
		return
	}
	s := m.shard(id)
	s.mu.Lock()
	if old, ok := s.m[id]; ok {
		m.order.Remove(old.elem)
		m.size -= entryCost(old.data)
	}
	e := &memEntry{id: id, data: data}
	e.elem = m.order.PushFront(e)
	s.m[id] = e
	m.size += cost
	s.mu.Unlock()
	m.evictLocked()
}

// evictLocked walks from the cold end until the cache fits its budget. A
// touched entry is cleared and moved to the front instead of dropped; the
// walk is bounded so a second pass over the same entry does evict it.
func (m *MemoryCache) evictLocked() {
	chances := m.order.Len()
	for m.size > m.capacity && m.order.Len() > 0 {
		back := m.order.Back()
		e := back.Value.(*memEntry)
		if chances > 0 && e.touched.CompareAndSwap(true, false) {
			m.order.MoveToFront(back)
			chances--
			continue
		}
		m.order.Remove(back)
		s := m.shard(e.id)
		s.mu.Lock()
		if s.m[e.id] == e {
			delete(s.m, e.id)
		}
		s.mu.Unlock()
		m.size -= entryCost(e.data)
	}
}

// SetCapacity changes the byte budget, evicting immediately when shrinking.
func (m *MemoryCache) SetCapacity(maxBytes int64) {
	if m == nil {
		return
	}
	m.orderMu.Lock()
	defer m.orderMu.Unlock()
	m.capacity = max(maxBytes, 0)
	m.evictLocked()
}

func (m *MemoryCache) Capacity() int64 {
	if m == nil {
		return 0
	}
	m.orderMu.Lock()
	defer m.orderMu.Unlock()
	return m.capacity
}

// Size is the bytes currently charged to the budget.
func (m *MemoryCache) Size() int64 {
	if m == nil {
		return 0
	}
	m.orderMu.Lock()
	defer m.orderMu.Unlock()
	return m.size
}

// Len is the number of cached articles.
func (m *MemoryCache) Len() int {
	if m == nil {
		return 0
	}
	m.orderMu.Lock()
	defer m.orderMu.Unlock()
	return m.order.Len()
}
