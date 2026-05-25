// Package segstore provides small, composable usenet.SegmentStore
// implementations used to decouple PAR2 self-heal from the on-disk segment
// cache: an in-memory landing zone for reconstructed segments (MemStore) and a
// read-through chain that lets the reader consult several stores (Chain).
package segstore

import (
	"container/list"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/usenet"
)

// MemStore is an in-memory, size- and TTL-bounded usenet.SegmentStore. It backs
// PAR2 self-heal as a small "landing zone": reconstructed segments live here
// only until the streaming client re-reads them, so it needs neither disk nor
// persistence. Because it is independent of the on-disk segment cache,
// self-heal works in rclone-VFS-cache deployments where the segment cache is
// intentionally disabled. Safe for concurrent use.
type MemStore struct {
	mu       sync.Mutex
	maxBytes int64
	ttl      time.Duration
	curBytes int64
	ll       *list.List // most-recently-used at the front
	items    map[string]*list.Element
}

type memEntry struct {
	id    string
	data  []byte
	added time.Time
}

// DefaultMemStoreBytes / DefaultMemStoreTTL are used when non-positive values
// are passed to NewMemStore.
const (
	DefaultMemStoreBytes = 512 << 20 // 512 MiB
	DefaultMemStoreTTL   = time.Hour
)

// NewMemStore creates a store bounded to maxBytes of total payload, evicting the
// least-recently-used entries past that budget and lazily dropping entries older
// than ttl. Non-positive arguments fall back to the package defaults.
func NewMemStore(maxBytes int64, ttl time.Duration) *MemStore {
	if maxBytes <= 0 {
		maxBytes = DefaultMemStoreBytes
	}
	if ttl <= 0 {
		ttl = DefaultMemStoreTTL
	}
	return &MemStore{
		maxBytes: maxBytes,
		ttl:      ttl,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
	}
}

// Get returns a copy of the stored segment bytes, or (nil, false) on a miss or
// an expired entry (which it drops).
func (m *MemStore) Get(id string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.items[id]
	if !ok {
		return nil, false
	}
	e := el.Value.(*memEntry)
	if time.Since(e.added) > m.ttl {
		m.removeLocked(el)
		return nil, false
	}
	m.ll.MoveToFront(el)
	out := make([]byte, len(e.data))
	copy(out, e.data)
	return out, true
}

// Put stores a copy of data under id, refreshing an existing entry, then evicts
// least-recently-used entries until within the byte budget.
func (m *MemStore) Put(id string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[id]; ok {
		e := el.Value.(*memEntry)
		m.curBytes -= int64(len(e.data))
		e.data = append([]byte(nil), data...)
		e.added = time.Now()
		m.curBytes += int64(len(e.data))
		m.ll.MoveToFront(el)
	} else {
		e := &memEntry{id: id, data: append([]byte(nil), data...), added: time.Now()}
		m.items[id] = m.ll.PushFront(e)
		m.curBytes += int64(len(e.data))
	}
	for m.curBytes > m.maxBytes {
		back := m.ll.Back()
		if back == nil {
			break
		}
		m.removeLocked(back)
	}
	return nil
}

// Len reports the number of live entries (for tests/metrics).
func (m *MemStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.items)
}

func (m *MemStore) removeLocked(el *list.Element) {
	e := el.Value.(*memEntry)
	m.ll.Remove(el)
	delete(m.items, e.id)
	m.curBytes -= int64(len(e.data))
}

// Ensure MemStore satisfies the reader's store interface.
var _ usenet.SegmentStore = (*MemStore)(nil)
