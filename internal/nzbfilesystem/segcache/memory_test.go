package segcache

import (
	"bytes"
	"fmt"
	"testing"
)

func mb(n int) []byte { return bytes.Repeat([]byte{1}, n<<20) }

func TestMemoryCacheHitAndMiss(t *testing.T) {
	c := NewMemoryCache(8 << 20)
	if _, ok := c.Get("a"); ok {
		t.Fatal("empty cache must miss")
	}
	c.Put("a", []byte{1, 2, 3})
	got, ok := c.Get("a")
	if !ok || !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("hit: %v %v", got, ok)
	}
}

func TestMemoryCacheStaysWithinBudget(t *testing.T) {
	c := NewMemoryCache(4 << 20)
	for i := 0; i < 10; i++ {
		c.Put(fmt.Sprint(i), mb(1))
	}
	if c.Len() > 3 || c.Size() > 4<<20 {
		t.Fatalf("len=%d size=%d over a 4 MB budget", c.Len(), c.Size())
	}
	if _, ok := c.Get("9"); !ok {
		t.Fatal("the newest entry must survive")
	}
}

func TestMemoryCacheSecondChanceKeepsTouchedEntries(t *testing.T) {
	c := NewMemoryCache(3<<20 + 3*memEntryOverhead)
	c.Put("old-touched", mb(1))
	c.Put("old-cold", mb(1))
	c.Put("mid", mb(1))
	c.Get("old-touched") // marks it recently used
	c.Put("new", mb(1))  // forces one eviction
	if _, ok := c.Get("old-touched"); !ok {
		t.Fatal("a touched entry must get a second chance")
	}
	if _, ok := c.Get("old-cold"); ok {
		t.Fatal("the untouched oldest entry must be the one evicted")
	}
}

func TestMemoryCacheSkipsOversizeAndReplacesInPlace(t *testing.T) {
	c := NewMemoryCache(2 << 20)
	c.Put("huge", mb(3))
	if c.Len() != 0 {
		t.Fatal("an article larger than the budget must not be cached")
	}
	c.Put("a", mb(1))
	c.Put("a", mb(1))
	if c.Len() != 1 || c.Size() != int64(1<<20)+memEntryOverhead {
		t.Fatalf("replace must not leak: len=%d size=%d", c.Len(), c.Size())
	}
}

func TestMemoryCacheShrinkEvicts(t *testing.T) {
	c := NewMemoryCache(8 << 20)
	for i := 0; i < 6; i++ {
		c.Put(fmt.Sprint(i), mb(1))
	}
	c.SetCapacity(2 << 20)
	if c.Len() > 1 || c.Size() > 2<<20 {
		t.Fatalf("shrink must evict: len=%d size=%d", c.Len(), c.Size())
	}
	var nilCache *MemoryCache
	nilCache.Put("x", mb(1))
	if _, ok := nilCache.Get("x"); ok {
		t.Fatal("nil cache must be inert")
	}
}
