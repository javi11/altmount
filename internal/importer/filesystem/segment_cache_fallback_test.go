package filesystem

import (
	"bytes"
	"testing"
)

type mapStore struct {
	data map[string][]byte
	gets int
}

func (m *mapStore) Get(id string) ([]byte, bool) {
	m.gets++
	d, ok := m.data[id]
	return d, ok
}
func (m *mapStore) Put(id string, data []byte) error { m.data[id] = data; return nil }

// Articles the import warm-up already fetched live in the streaming segment
// store; an analysis pass that misses its own cache should look there before
// paying a provider round trip, and keep what it finds for its next probe.
func TestImportSegmentCacheReadsThroughFallback(t *testing.T) {
	article := bytes.Repeat([]byte("h"), 4096)
	fallback := &mapStore{data: map[string][]byte{"head-0": article}}
	c := NewImportSegmentCache(0).WithFallback(fallback)

	got, ok := c.Get("head-0")
	if !ok || !bytes.Equal(got, article) {
		t.Fatalf("Get(head-0) = (%d bytes, %v), want the fallback's article", len(got), ok)
	}
	if _, ok := c.Get("head-0"); !ok {
		t.Fatal("second Get(head-0) missed: fallback hits must be kept locally")
	}
	if fallback.gets != 1 {
		t.Fatalf("fallback consulted %d times, want 1", fallback.gets)
	}
	if s := c.Stats(); s.Hits != 2 || s.Misses != 0 {
		t.Fatalf("stats = hits %d misses %d, want 2/0: a fallback hit is a hit", s.Hits, s.Misses)
	}
	if _, ok := c.Get("absent"); ok {
		t.Fatal("Get(absent) hit")
	}
	if s := c.Stats(); s.Misses != 1 {
		t.Fatalf("misses = %d, want 1 after an id neither tier has", s.Misses)
	}
}
