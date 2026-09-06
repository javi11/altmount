package filesystem

import (
	"fmt"
	"testing"
)

// The bound is this cache's only safety property — without it an archive with
// enough volumes would grow it until the importer OOMs. Nothing pinned it.
func TestImportSegmentCache_EvictsToStayUnderBound(t *testing.T) {
	const seg = 1 << 20 // 1 MiB
	const bound = 8 * seg

	c := NewImportSegmentCache(bound)
	for i := range 32 { // 4x the bound
		if err := c.Put(fmt.Sprintf("m%02d@h", i), make([]byte, seg)); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}

	if c.curBytes > c.maxBytes {
		t.Errorf("cache holds %d bytes, exceeds its bound of %d", c.curBytes, c.maxBytes)
	}
	if c.curBytes == 0 {
		t.Error("cache evicted everything; it should retain up to its bound")
	}
}

func TestImportSegmentCache_EvictsLeastRecentlyUsed(t *testing.T) {
	const seg = 1 << 20
	c := NewImportSegmentCache(3 * seg)

	for _, id := range []string{"a", "b", "c"} {
		if err := c.Put(id, make([]byte, seg)); err != nil {
			t.Fatal(err)
		}
	}
	// Touch "a" so "b" becomes the least recently used.
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should still be cached")
	}
	if err := c.Put("d", make([]byte, seg)); err != nil {
		t.Fatal(err)
	}

	if _, ok := c.Get("b"); ok {
		t.Error("b was least recently used and should have been evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Error("a was touched after b and should have been retained")
	}
	if _, ok := c.Get("d"); !ok {
		t.Error("d was just inserted and should be present")
	}
}

// TestImportSegmentCache_WorstCaseFootprintIsDeliberate pins the memory budget
// rather than the cache's behaviour. Five construction sites exist, and the
// nested-RAR pass runs INSIDE the outer pass, so two caches are live per import;
// with max_processor_workers at 2 that is four concurrent instances. AltMount
// commonly runs on NAS hardware, so raising the default is a decision that
// should be made deliberately, not drifted into.
func TestImportSegmentCache_WorstCaseFootprintIsDeliberate(t *testing.T) {
	const concurrentInstances = 4 // 2 nested passes x max_processor_workers 2
	const budget = 256 << 20      // what we are willing to spend in the worst case

	if got := DefaultImportSegmentCacheBytes * concurrentInstances; got > budget {
		t.Errorf("worst-case cache footprint %d MiB exceeds the %d MiB budget; "+
			"raising DefaultImportSegmentCacheBytes must be a deliberate decision",
			got>>20, budget>>20)
	}
}
