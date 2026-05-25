package segstore

import (
	"testing"
	"time"

	"github.com/javi11/altmount/internal/usenet"
)

func TestMemStoreGetPutCopies(t *testing.T) {
	m := NewMemStore(1<<20, time.Hour)
	orig := []byte("hello")
	if err := m.Put("a", orig); err != nil {
		t.Fatalf("Put: %v", err)
	}
	orig[0] = 'X' // mutating the source must not affect the stored copy

	got, ok := m.Get("a")
	if !ok || string(got) != "hello" {
		t.Fatalf("Get = %q,%v; want hello,true", got, ok)
	}
	got[0] = 'Y' // mutating the returned slice must not affect the store
	if again, _ := m.Get("a"); string(again) != "hello" {
		t.Fatalf("second Get = %q; want hello (Get must return a copy)", again)
	}
}

func TestMemStoreTTLExpiry(t *testing.T) {
	m := NewMemStore(1<<20, 5*time.Millisecond)
	_ = m.Put("a", []byte("x"))
	if _, ok := m.Get("a"); !ok {
		t.Fatal("entry should be present before TTL")
	}
	time.Sleep(15 * time.Millisecond)
	if _, ok := m.Get("a"); ok {
		t.Fatal("entry should have expired after TTL")
	}
	if m.Len() != 0 {
		t.Fatalf("expired entry not dropped: Len=%d", m.Len())
	}
}

func TestMemStoreSizeEvictionLRU(t *testing.T) {
	// Budget holds two 5-byte entries.
	m := NewMemStore(10, time.Hour)
	_ = m.Put("a", []byte("aaaaa"))
	_ = m.Put("b", []byte("bbbbb"))
	// Touch "a" so "b" becomes least-recently-used.
	if _, ok := m.Get("a"); !ok {
		t.Fatal("a should be present")
	}
	_ = m.Put("c", []byte("ccccc")) // over budget → evict LRU ("b")

	if _, ok := m.Get("b"); ok {
		t.Fatal("b should have been evicted as least-recently-used")
	}
	if _, ok := m.Get("a"); !ok {
		t.Fatal("a should have survived (recently used)")
	}
	if _, ok := m.Get("c"); !ok {
		t.Fatal("c should be present (just inserted)")
	}
}

func TestNewChainNilWhenEmpty(t *testing.T) {
	if c := NewChain(nil, nil); c != nil {
		t.Fatal("NewChain(nil,nil) must return nil to preserve the no-store fast path")
	}
}

func TestChainReadOrderAndWriteTarget(t *testing.T) {
	cache := NewMemStore(1<<20, time.Hour)
	repair := NewMemStore(1<<20, time.Hour)

	// Recovered segment lives only in the repair store.
	_ = repair.Put("recovered", []byte("fixed"))
	// A segment present in both — cache must win.
	_ = cache.Put("dup", []byte("from-cache"))
	_ = repair.Put("dup", []byte("from-repair"))

	var c usenet.SegmentStore = NewChain(cache, repair)

	if got, ok := c.Get("recovered"); !ok || string(got) != "fixed" {
		t.Fatalf("repair fallback Get = %q,%v; want fixed,true", got, ok)
	}
	if got, _ := c.Get("dup"); string(got) != "from-cache" {
		t.Fatalf("read order Get = %q; want from-cache (cache precedes repair)", got)
	}

	// A tee-write goes to the cache only, never the repair store.
	if err := c.Put("fetched", []byte("net")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := cache.Get("fetched"); !ok {
		t.Fatal("tee-write should land in the cache")
	}
	if _, ok := repair.Get("fetched"); ok {
		t.Fatal("tee-write must NOT pollute the repair store")
	}
}

func TestChainRepairOnlyPutIsNoop(t *testing.T) {
	repair := NewMemStore(1<<20, time.Hour)
	c := NewChain(nil, repair) // cache disabled

	if _, ok := c.Get("x"); ok {
		t.Fatal("unexpected hit")
	}
	// With no cache, a tee-write has nowhere safe to go and must be a no-op
	// (so normal fetches can't evict recovered segments from the small store).
	if err := c.Put("x", []byte("net")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := repair.Get("x"); ok {
		t.Fatal("tee-write must not land in the repair store when cache is off")
	}
	// But reads still resolve recovered segments written directly to repair.
	_ = repair.Put("r", []byte("fixed"))
	if got, ok := c.Get("r"); !ok || string(got) != "fixed" {
		t.Fatalf("Get = %q,%v; want fixed,true", got, ok)
	}
}
