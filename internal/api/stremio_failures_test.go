package api

import (
	"sync"
	"testing"
	"time"
)

func TestStremioFailureCache_RecordHasForget(t *testing.T) {
	c := newStremioFailureCache()
	const ttl = time.Hour

	if c.Has("nothing", ttl) {
		t.Error("empty cache reported a hit")
	}

	c.Record("Some.Release.1080p")
	if !c.Has("Some.Release.1080p", ttl) {
		t.Error("recorded key not reported as failed")
	}
	if c.Has("Other.Release", ttl) {
		t.Error("unrelated key reported as failed")
	}

	// A release that later plays must stop being excluded.
	c.Forget("Some.Release.1080p")
	if c.Has("Some.Release.1080p", ttl) {
		t.Error("forgotten key still reported as failed")
	}

	// Empty keys are ignored rather than poisoning the cache.
	c.Record("")
	if c.Has("", ttl) {
		t.Error("empty key reported as failed")
	}
}

func TestStremioFailureCache_TTL(t *testing.T) {
	c := newStremioFailureCache()
	c.Record("Aged.Release")

	// Backdate the entry so it is two hours old.
	c.mu.Lock()
	c.items["Aged.Release"] = time.Now().Add(-2 * time.Hour)
	c.mu.Unlock()

	tests := []struct {
		name string
		ttl  time.Duration
		want bool
	}{
		{name: "within ttl", ttl: 3 * time.Hour, want: true},
		{name: "past ttl", ttl: time.Hour, want: false},
		{name: "zero ttl never expires", ttl: 0, want: true},
		{name: "negative ttl never expires", ttl: -1, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Has("Aged.Release", tt.ttl); got != tt.want {
				t.Errorf("Has = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestStremioFailureCache_KeysPurgesExpired(t *testing.T) {
	c := newStremioFailureCache()
	c.Record("Fresh")
	c.Record("Stale")

	c.mu.Lock()
	c.items["Stale"] = time.Now().Add(-48 * time.Hour)
	c.mu.Unlock()

	keys := c.Keys(24 * time.Hour)
	if _, ok := keys["Fresh"]; !ok {
		t.Error("fresh key missing from Keys")
	}
	if _, ok := keys["Stale"]; ok {
		t.Error("stale key returned by Keys")
	}

	// Keys purges lazily, which is why the cache needs no background goroutine.
	c.mu.RLock()
	_, stillStored := c.items["Stale"]
	c.mu.RUnlock()
	if stillStored {
		t.Error("expired entry was not purged from the backing map")
	}
}

func TestStremioFailureCache_EvictsOldestWhenFull(t *testing.T) {
	c := newStremioFailureCache()

	base := time.Now()
	for i := range stremioFailureCacheMaxEntries {
		key := "release-" + string(rune('a'+i%26)) + "-" + time.Duration(i).String()
		c.Record(key)
		// Force a strictly increasing age ordering so "oldest" is deterministic.
		c.mu.Lock()
		c.items[key] = base.Add(time.Duration(i) * time.Millisecond)
		c.mu.Unlock()
	}

	c.mu.RLock()
	size := len(c.items)
	c.mu.RUnlock()
	if size != stremioFailureCacheMaxEntries {
		t.Fatalf("expected %d entries, got %d", stremioFailureCacheMaxEntries, size)
	}

	// The oldest entry is the one stamped at base.
	oldest := "release-a-0s"
	c.Record("brand-new")

	c.mu.RLock()
	size = len(c.items)
	_, oldestPresent := c.items[oldest]
	_, newPresent := c.items["brand-new"]
	c.mu.RUnlock()

	if size != stremioFailureCacheMaxEntries {
		t.Errorf("cache grew past cap: %d entries", size)
	}
	if oldestPresent {
		t.Error("oldest entry was not evicted")
	}
	if !newPresent {
		t.Error("new entry was not stored")
	}
}

func TestStremioFailureCache_ConcurrentAccess(t *testing.T) {
	c := newStremioFailureCache()
	const ttl = time.Hour

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "release-" + time.Duration(i).String()
			c.Record(key)
			c.Has(key, ttl)
			c.Keys(ttl)
			if i%3 == 0 {
				c.Forget(key)
			}
		}(i)
	}
	wg.Wait()
}
