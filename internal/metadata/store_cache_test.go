package metadata

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bigStore builds a store whose segment count is representative of a real
// release (thousands of message IDs), which is what makes the decoded proto
// expensive to retain.
func bigStore(segCount int) *metapb.NzbStore {
	segs := make([]*metapb.NzbSeg, segCount)
	for i := range segs {
		segs[i] = &metapb.NzbSeg{
			Id:     fmt.Sprintf("part%dof%d.abcdef0123456789abcdef@powerpost.local", i+1, segCount),
			Number: int32(i + 1),
			Bytes:  760000,
		}
	}
	return &metapb.NzbStore{Files: []*metapb.NzbFileEntry{{
		Subject:  "Some.Release.S01E01.1080p.WEB.h264 yEnc (1/1)",
		Poster:   "poster@example.invalid",
		Groups:   []string{"alt.binaries.test"},
		Segments: segs,
	}}}
}

// writeStores writes n stores of segCount segments each and returns their refs,
// leaving the cache empty so the caller controls what gets faulted in.
func writeStores(t *testing.T, ss *StoreService, dir string, n, segCount int) []string {
	t.Helper()
	refs := make([]string, n)
	for i := range refs {
		ref := filepath.Join(dir, fmt.Sprintf("rel%d.nzbz", i))
		require.NoError(t, ss.WriteStore(ref, bigStore(segCount)))
		refs[i] = ref
	}
	ss.purgeCache()
	return refs
}

// A store LRU bounded only by entry count retains an unbounded number of
// bytes: 256 large releases is hundreds of MB of live heap (issue #819).
// Retention must be bounded by size, not by entry count.
func TestStoreCache_BoundedByBytes(t *testing.T) {
	dir := t.TempDir()
	const budget = 8 << 20
	ss := newStoreServiceWithBudget(dir, budget)

	refs := writeStores(t, ss, dir, 60, 5000)
	for _, ref := range refs {
		_, err := ss.ReadStore(ref)
		require.NoError(t, err)
	}

	assert.LessOrEqual(t, ss.cachedBytes(), int64(budget),
		"cache must stay within its byte budget")
	assert.Less(t, ss.cache.Len(), len(refs),
		"a byte-bounded cache must have evicted entries")
	assert.Positive(t, ss.cache.Len(), "cache must still retain the hot entries")
}

// The most recently read store stays cached: eviction targets the cold end.
func TestStoreCache_KeepsHotEntry(t *testing.T) {
	dir := t.TempDir()
	ss := newStoreServiceWithBudget(dir, 8<<20)

	refs := writeStores(t, ss, dir, 20, 5000)
	for _, ref := range refs {
		_, err := ss.ReadStore(ref)
		require.NoError(t, err)
	}

	assert.True(t, ss.cache.Contains(refs[len(refs)-1]),
		"most recently read store must survive eviction")
}

// A single store larger than the whole budget must still be served, and must
// not leave the accounting permanently over budget once it is evicted.
func TestStoreCache_OversizedStore(t *testing.T) {
	dir := t.TempDir()
	ss := newStoreServiceWithBudget(dir, 1<<10) // absurdly small

	refs := writeStores(t, ss, dir, 2, 500)
	for _, ref := range refs {
		got, err := ss.ReadStore(ref)
		require.NoError(t, err)
		require.Len(t, got.Files[0].Segments, 500, "oversized store must still be returned")
	}

	assert.LessOrEqual(t, ss.cache.Len(), 1,
		"an oversized store must not accumulate alongside others")
	ss.purgeCache()
	assert.Zero(t, ss.cachedBytes(), "purge must reset the byte accounting")
}

// Writes are cached too, and must obey the same budget.
func TestStoreCache_WriteObeysBudget(t *testing.T) {
	dir := t.TempDir()
	const budget = 4 << 20
	ss := newStoreServiceWithBudget(dir, budget)

	for i := range 40 {
		ref := filepath.Join(dir, fmt.Sprintf("w%d.nzbz", i))
		require.NoError(t, ss.WriteStore(ref, bigStore(5000)))
	}

	assert.LessOrEqual(t, ss.cachedBytes(), int64(budget))
}

// Every entry costs something regardless of payload, so a flood of tiny
// stores cannot multiply LRU nodes and map slots free of charge.
func TestStoreCache_ChargesPerEntryOverhead(t *testing.T) {
	empty := estimateStoreBytes("/meta/rel.nzbz", &metapb.NzbStore{})
	assert.Positive(t, empty, "an empty store must still be charged for its entry")
	assert.Greater(t, estimateStoreBytes("/meta/a-much-longer-store-ref.nzbz", &metapb.NzbStore{}), empty,
		"the ref key is part of the entry's cost")
	assert.Positive(t, estimateStoreBytes("/meta/rel.nzbz", nil), "a nil store still occupies an entry")
}

// End-to-end guard on the reported symptom: faulting in far more store bytes
// than the budget must not grow live heap without bound.
func TestStoreCache_LiveHeapStaysBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("heap measurement is slow")
	}
	dir := t.TempDir()
	ss := newStoreServiceWithBudget(dir, defaultStoreCacheBytes)
	refs := writeStores(t, ss, dir, 300, 5000)

	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for _, ref := range refs {
		_, err := ss.ReadStore(ref)
		require.NoError(t, err)
	}

	runtime.GC()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	retainedMB := (float64(after.HeapAlloc) - float64(before.HeapAlloc)) / (1 << 20)
	t.Logf("live heap retained by store cache: %.1f MB (budget %d MB, %d entries)",
		retainedMB, defaultStoreCacheBytes>>20, ss.cache.Len())

	// Unbounded-by-entry-count retention measured ~190 MB here; the estimator
	// is deliberately conservative, so allow generous headroom over the budget
	// while still failing loudly on a regression to entry-count bounding.
	limit := float64(defaultStoreCacheBytes>>20) * 1.5
	assert.Less(t, retainedMB, limit, "store cache retention must track its byte budget")
}
