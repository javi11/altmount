package metadata

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStoreRefCounter mirrors StoreRefRepository semantics in-memory: a store is
// "tracked" once incremented, and its row disappears when the count reaches zero.
type fakeStoreRefCounter struct {
	counts map[string]int64
}

func newFakeStoreRefCounter() *fakeStoreRefCounter {
	return &fakeStoreRefCounter{counts: make(map[string]int64)}
}

func (f *fakeStoreRefCounter) IncStoreRef(_ context.Context, storePath string) error {
	f.counts[storePath]++
	return nil
}

func (f *fakeStoreRefCounter) DecStoreRef(_ context.Context, storePath string) (int64, bool, error) {
	count, ok := f.counts[storePath]
	if !ok {
		return 0, false, nil
	}
	count--
	if count <= 0 {
		delete(f.counts, storePath)
		return 0, true, nil
	}
	f.counts[storePath] = count
	return count, true, nil
}

func (f *fakeStoreRefCounter) tracked(storePath string) bool {
	_, ok := f.counts[storePath]
	return ok
}

// writeSharedRelease writes n v3 metadata files that all point at a single .nzbz
// store, exactly as a season-pack import does, and returns their virtual paths.
func writeSharedRelease(t *testing.T, ms *MetadataService, storeRef string, names ...string) []string {
	t.Helper()
	return writeSharedReleaseIn(t, ms, storeRef, "tv", names...)
}

// writeSharedReleaseIn is writeSharedRelease with an explicit parent directory, for
// cases where one store is referenced from more than one directory.
func writeSharedReleaseIn(t *testing.T, ms *MetadataService, storeRef, dir string, names ...string) []string {
	t.Helper()

	segs := make([]*metapb.NzbSeg, len(names))
	for i, name := range names {
		segs[i] = &metapb.NzbSeg{Id: name + "@n", Number: int32(i + 1), Bytes: 100}
	}
	store := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{{
		Subject:  "Season Pack",
		Segments: segs,
	}}}
	require.NoError(t, ms.Store().WriteStore(storeRef, store))

	paths := make([]string, len(names))
	for i, name := range names {
		vpath := filepath.Join(dir, name)
		paths[i] = vpath
		meta := &metapb.FileMetadata{
			FileSize: 100,
			Status:   metapb.FileStatus_FILE_STATUS_HEALTHY,
			StoreRef: storeRef,
			// v3 aliases source_nzb_path onto the store; the raw .nzb is long gone.
			SourceNzbPath: storeRef,
			SegmentRefs: []*metapb.SegmentRef{
				{StoreIndex: int64(i), StartOffset: 0, EndOffset: 99},
			},
		}
		require.NoError(t, ms.WriteFileMetadata(vpath, meta))
		ms.IncStoreRef(context.Background(), storeRef)
	}
	return paths
}

func TestDeleteFileMetadata_SharedStoreSurvivesWhileSiblingsReference(t *testing.T) {
	ms := NewMetadataService(t.TempDir())
	refs := newFakeStoreRefCounter()
	ms.SetStoreRefCounter(refs)

	storeRef := filepath.Join(t.TempDir(), "10767-release.nzbz")
	paths := writeSharedRelease(t, ms, storeRef, "s01e01.mkv", "s01e02.mkv", "s01e03.mkv")

	ctx := context.Background()
	require.NoError(t, ms.DeleteFileMetadata(ctx, paths[0]))

	assert.FileExists(t, storeRef, "shared store must survive while siblings still reference it")
	assert.Equal(t, int64(2), refs.counts[storeRef], "ref count must drop by exactly one")

	assert.NoFileExists(t, ms.GetMetadataFilePath(paths[0]))
	for _, survivor := range paths[1:] {
		got, err := ms.ReadFileMetadata(survivor)
		require.NoError(t, err, "surviving metadata must remain readable")
		require.NotNil(t, got)
		assert.Len(t, got.SegmentData, 1)
	}
}

func TestDeleteFileMetadata_StoreRemovedOnLastReference(t *testing.T) {
	ms := NewMetadataService(t.TempDir())
	refs := newFakeStoreRefCounter()
	ms.SetStoreRefCounter(refs)

	storeRef := filepath.Join(t.TempDir(), "10767-release.nzbz")
	paths := writeSharedRelease(t, ms, storeRef, "s01e01.mkv", "s01e02.mkv")

	ctx := context.Background()
	for _, p := range paths {
		require.NoError(t, ms.DeleteFileMetadata(ctx, p))
	}

	assert.NoFileExists(t, storeRef, "store must be removed once the last reference is gone")
	assert.False(t, refs.tracked(storeRef), "ref count row must be removed with the store")
}

func TestDeleteFileMetadata_UntrackedStoreIsNotRemoved(t *testing.T) {
	ms := NewMetadataService(t.TempDir())
	refs := newFakeStoreRefCounter()

	storeRef := filepath.Join(t.TempDir(), "premigration.nzbz")
	// Written before the refcounter exists, as v3 metadata predating migration 032 was.
	paths := writeSharedRelease(t, ms, storeRef, "s01e01.mkv", "s01e02.mkv")
	ms.SetStoreRefCounter(refs)

	ctx := context.Background()
	require.NoError(t, ms.DeleteFileMetadata(ctx, paths[0]))

	assert.FileExists(t, storeRef,
		"an untracked store must be left alone: a missing row means unknown refs, not zero refs")

	got, err := ms.ReadFileMetadata(paths[1])
	require.NoError(t, err)
	require.NotNil(t, got)
}

func TestDeleteFileMetadata_DecrementsWhenStoreFileAlreadyGone(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	refs := newFakeStoreRefCounter()
	ms.SetStoreRefCounter(refs)

	storeRef := filepath.Join(t.TempDir(), "10767-release.nzbz")
	paths := writeSharedRelease(t, ms, storeRef, "s01e01.mkv", "s01e02.mkv")

	// Reproduces an install already damaged by the old purge behaviour: the store
	// is gone but the refcount rows remain. A second service starts with a cold
	// store cache, so reading this metadata genuinely fails the way it does after
	// a restart — deletion must still decrement rather than stranding the count.
	require.NoError(t, os.Remove(storeRef))
	restarted := NewMetadataService(root)
	restarted.SetStoreRefCounter(refs)

	ctx := context.Background()
	_, readErr := restarted.ReadFileMetadata(paths[0])
	require.Error(t, readErr, "test premise: the metadata must be unreadable without its store")

	require.NoError(t, restarted.DeleteFileMetadata(ctx, paths[0]))

	assert.Equal(t, int64(1), refs.counts[storeRef],
		"deletion must decrement even when the store file cannot be read")
}

func TestDeleteDirectory_UntrackedStoreIsNotRemoved(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	refs := newFakeStoreRefCounter()

	storeRef := filepath.Join(t.TempDir(), "premigration.nzbz")
	// Written before the refcounter exists, as v3 metadata predating migration 032 was.
	writeSharedRelease(t, ms, storeRef, "s01e01.mkv", "s01e02.mkv")
	ms.SetStoreRefCounter(refs)

	require.NoError(t, ms.DeleteDirectory("tv"))

	assert.FileExists(t, storeRef,
		"an untracked store must be left alone: a missing row means unknown refs, not zero refs")
}

func TestDeleteDirectory_RemovesStoreWhenDirectoryHeldEveryReference(t *testing.T) {
	ms := NewMetadataService(t.TempDir())
	refs := newFakeStoreRefCounter()
	ms.SetStoreRefCounter(refs)

	storeRef := filepath.Join(t.TempDir(), "10767-release.nzbz")
	writeSharedRelease(t, ms, storeRef, "s01e01.mkv", "s01e02.mkv", "s01e03.mkv")

	require.NoError(t, ms.DeleteDirectory("tv"))

	assert.NoFileExists(t, storeRef, "store must be removed once the directory held its last reference")
	assert.False(t, refs.tracked(storeRef), "ref count row must be removed with the store")
}

func TestDeleteDirectory_KeepsStoreReferencedFromOutsideTheDirectory(t *testing.T) {
	ms := NewMetadataService(t.TempDir())
	refs := newFakeStoreRefCounter()
	ms.SetStoreRefCounter(refs)

	storeRef := filepath.Join(t.TempDir(), "10767-release.nzbz")
	writeSharedReleaseIn(t, ms, storeRef, "tv", "s01e01.mkv", "s01e02.mkv")
	outside := writeSharedReleaseIn(t, ms, storeRef, "movies", "extra.mkv")

	require.NoError(t, ms.DeleteDirectory("tv"))

	assert.FileExists(t, storeRef, "store must survive while metadata outside the directory references it")
	assert.Equal(t, int64(1), refs.counts[storeRef], "only the references inside the directory may be dropped")

	got, err := ms.ReadFileMetadata(outside[0])
	require.NoError(t, err, "metadata outside the deleted directory must remain readable")
	require.NotNil(t, got)
}
