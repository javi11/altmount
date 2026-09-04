package segcache

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeDisk struct {
	mu   sync.Mutex
	m    map[string][]byte
	gets atomic.Int64
}

func newFakeDisk() *fakeDisk { return &fakeDisk{m: map[string][]byte{}} }

func (d *fakeDisk) Get(id string) ([]byte, bool) {
	d.gets.Add(1)
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.m[id]
	return v, ok
}

func (d *fakeDisk) Put(id string, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.m[id] = data
	return nil
}

func (d *fakeDisk) has(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.m[id]
	return ok
}

func TestTieredMemoryHitAvoidsDisk(t *testing.T) {
	disk := newFakeDisk()
	ts := NewTieredStore(NewMemoryCache(8 << 20))
	ts.SetDisk(disk)
	_ = ts.Put("a", []byte{1})
	if _, ok := ts.Get("a"); !ok || disk.gets.Load() != 0 {
		t.Fatalf("memory hit must not touch disk (gets=%d)", disk.gets.Load())
	}
}

func TestTieredDiskHitIsPromoted(t *testing.T) {
	disk := newFakeDisk()
	_ = disk.Put("b", []byte{2})
	ts := NewTieredStore(NewMemoryCache(8 << 20))
	ts.SetDisk(disk)
	if _, ok := ts.Get("b"); !ok {
		t.Fatal("disk hit expected")
	}
	if _, ok := ts.Get("b"); !ok || disk.gets.Load() != 1 {
		t.Fatalf("second lookup must be served from memory (disk gets=%d)", disk.gets.Load())
	}
}

func TestTieredPutReachesDiskAsynchronously(t *testing.T) {
	disk := newFakeDisk()
	ts := NewTieredStore(NewMemoryCache(8 << 20))
	ts.SetDisk(disk)
	_ = ts.Put("c", []byte{3})
	deadline := time.Now().Add(2 * time.Second)
	for !disk.has("c") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !disk.has("c") {
		t.Fatal("disk write never arrived")
	}
}

func TestTieredWithoutDiskIsMemoryOnly(t *testing.T) {
	ts := NewTieredStore(NewMemoryCache(8 << 20))
	_ = ts.Put("d", []byte{4})
	if _, ok := ts.Get("d"); !ok || ts.Dropped() != 0 {
		t.Fatal("memory-only store must serve and not count drops")
	}
	if _, ok := ts.Get("missing"); ok {
		t.Fatal("miss expected")
	}
}
