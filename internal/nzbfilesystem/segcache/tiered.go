package segcache

import (
	"sync/atomic"

	"github.com/javi11/altmount/internal/usenet"
)

// TieredStore is the SegmentStore readers see: a memory tier in front of an
// optional disk tier. Reads try memory, then disk (promoting the hit);
// writes land in memory at once and reach disk through a bounded async
// writer, so a 750 KB file write never sits on the download goroutine.
type TieredStore struct {
	mem     *MemoryCache
	disk    atomic.Pointer[diskRef]
	writes  chan diskWrite
	dropped atomic.Int64
}

type diskRef struct{ store usenet.SegmentStore }

type diskWrite struct {
	id   string
	data []byte
}

// diskWriteQueue bounds how many decoded articles wait for the disk. A full
// queue drops the write (the memory tier still has the bytes) rather than
// stalling playback behind a slow disk.
const diskWriteQueue = 64

func NewTieredStore(mem *MemoryCache) *TieredStore {
	t := &TieredStore{mem: mem, writes: make(chan diskWrite, diskWriteQueue)}
	go t.writer()
	return t
}

// SetDisk installs or removes (nil) the disk tier.
func (t *TieredStore) SetDisk(d usenet.SegmentStore) {
	if d == nil {
		t.disk.Store(nil)
		return
	}
	t.disk.Store(&diskRef{store: d})
}

func (t *TieredStore) diskStore() usenet.SegmentStore {
	if r := t.disk.Load(); r != nil {
		return r.store
	}
	return nil
}

// Get returns the article from memory, else from disk (promoting it).
func (t *TieredStore) Get(id string) ([]byte, bool) {
	if data, ok := t.mem.Get(id); ok {
		return data, true
	}
	d := t.diskStore()
	if d == nil {
		return nil, false
	}
	data, ok := d.Get(id)
	if ok {
		t.mem.Put(id, data)
	}
	return data, ok
}

// Put stores the article in memory now and on disk asynchronously.
func (t *TieredStore) Put(id string, data []byte) error {
	t.mem.Put(id, data)
	if t.diskStore() == nil {
		return nil
	}
	select {
	case t.writes <- diskWrite{id: id, data: data}:
	default:
		t.dropped.Add(1)
	}
	return nil
}

// Dropped counts disk writes skipped because the queue was full.
func (t *TieredStore) Dropped() int64 { return t.dropped.Load() }

// Memory exposes the memory tier for stats and capacity changes.
func (t *TieredStore) Memory() *MemoryCache { return t.mem }

func (t *TieredStore) writer() {
	for w := range t.writes {
		if d := t.diskStore(); d != nil {
			_ = d.Put(w.id, w.data)
		}
	}
}
