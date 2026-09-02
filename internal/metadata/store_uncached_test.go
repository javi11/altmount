package metadata

import (
	"os"
	"path/filepath"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadStoreUncached_ActuallyHitsDisk guards the post-write integrity check.
// WriteStore populates the cache and ReadStore consults it first, so a cached
// read proves only that the in-memory object still exists — it would happily
// pass even if the bytes on disk were truncated or never landed.
func TestReadStoreUncached_ActuallyHitsDisk(t *testing.T) {
	ss := NewStoreService(t.TempDir())
	ref := filepath.Join(t.TempDir(), "rel.nzbz")

	store := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Subject: "A.mkv", Segments: []*metapb.NzbSeg{{Id: "a@n", Number: 1, Bytes: 100}}},
	}}
	require.NoError(t, ss.WriteStore(ref, store))

	// Corrupt the file on disk. The cache still holds the good object.
	require.NoError(t, os.WriteFile(ref, []byte("not zstd"), 0644))

	cached, err := ss.ReadStore(ref)
	require.NoError(t, err, "cached read succeeds and therefore proves nothing about disk")
	require.Len(t, cached.Files, 1)

	_, err = ss.ReadStoreUncached(ref)
	require.Error(t, err, "an uncached read must surface the corruption")
	assert.Contains(t, err.Error(), "decompress")
}

// TestReadStoreUncached_DoesNotPolluteCache keeps the verification read from
// evicting live entries or masking a later corruption.
func TestReadStoreUncached_DoesNotPolluteCache(t *testing.T) {
	ss := NewStoreService(t.TempDir())
	ref := filepath.Join(t.TempDir(), "rel.nzbz")

	store := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Subject: "A.mkv", Segments: []*metapb.NzbSeg{{Id: "a@n", Number: 1, Bytes: 100}}},
	}}
	require.NoError(t, ss.WriteStore(ref, store))
	ss.purgeCache()

	got, err := ss.ReadStoreUncached(ref)
	require.NoError(t, err)
	require.Len(t, got.Files, 1)

	_, cached := ss.cacheGet(ref)
	assert.False(t, cached, "an uncached read must not populate the cache")
}
