package metadata

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// corruptedMetaPath mirrors where MoveToCorrupted parks a virtual path's .meta.
func corruptedMetaPath(root, virtualPath string) string {
	v := strings.TrimPrefix(filepath.ToSlash(virtualPath), "/")
	return filepath.Join(root, "corrupted_metadata", filepath.FromSlash(v)+".meta")
}

func ageFile(t *testing.T, path string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, when, when))
}

func TestPruneCorrupted_MissingFolderIsNotAnError(t *testing.T) {
	ms := NewMetadataService(t.TempDir())

	removed, err := ms.PruneCorrupted(context.Background(), 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

func TestPruneCorrupted_RemovesOnlyExpiredCopies(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	refs := newFakeStoreRefCounter()
	ms.SetStoreRefCounter(refs)

	storeRef := filepath.Join(t.TempDir(), "10767-release.nzbz")
	paths := writeSharedRelease(t, ms, storeRef, "s01e01.mkv", "s01e02.mkv")

	ctx := context.Background()
	for _, p := range paths {
		require.NoError(t, ms.MoveToCorrupted(ctx, p))
	}
	ageFile(t, corruptedMetaPath(root, paths[0]), 48*time.Hour)

	removed, err := ms.PruneCorrupted(ctx, 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)

	assert.NoFileExists(t, corruptedMetaPath(root, paths[0]))
	assert.FileExists(t, corruptedMetaPath(root, paths[1]), "a copy inside the retention window must survive")

	assert.FileExists(t, storeRef, "store must survive while the younger copy still references it")
	assert.Equal(t, int64(1), refs.counts[storeRef], "ref count must drop by exactly one")
}

func TestPruneCorrupted_ReleasesStoreOnLastCopy(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	refs := newFakeStoreRefCounter()
	ms.SetStoreRefCounter(refs)

	storeRef := filepath.Join(t.TempDir(), "10767-release.nzbz")
	paths := writeSharedRelease(t, ms, storeRef, "s01e01.mkv", "s01e02.mkv")

	ctx := context.Background()
	for _, p := range paths {
		require.NoError(t, ms.MoveToCorrupted(ctx, p))
	}

	removed, err := ms.PruneCorrupted(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, removed)

	assert.NoFileExists(t, storeRef, "store must be released once the last safety copy is pruned")
	assert.False(t, refs.tracked(storeRef))

	assert.NoDirExists(t, filepath.Join(root, "corrupted_metadata", "tv"), "emptied sub-directory must be cleaned up")
	assert.DirExists(t, filepath.Join(root, "corrupted_metadata"), "the safety folder itself must remain")
}

func TestPruneCorrupted_RemovesIDSidecar(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	storeRef := filepath.Join(t.TempDir(), "10767-release.nzbz")
	paths := writeSharedRelease(t, ms, storeRef, "s01e01.mkv")

	require.NoError(t, os.WriteFile(ms.GetMetadataFilePath(paths[0])+".id", []byte("nzbdav-id"), 0o644))

	ctx := context.Background()
	require.NoError(t, ms.MoveToCorrupted(ctx, paths[0]))

	copyPath := corruptedMetaPath(root, paths[0])
	require.FileExists(t, copyPath+".id")

	removed, err := ms.PruneCorrupted(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
	assert.NoFileExists(t, copyPath+".id")
}

func TestPruneCorrupted_RespectsContextCancellation(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	storeRef := filepath.Join(t.TempDir(), "10767-release.nzbz")
	paths := writeSharedRelease(t, ms, storeRef, "s01e01.mkv")
	require.NoError(t, ms.MoveToCorrupted(context.Background(), paths[0]))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	removed, err := ms.PruneCorrupted(ctx, 0)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, removed)
	assert.FileExists(t, corruptedMetaPath(root, paths[0]), "a cancelled sweep must not remove anything")
}

func TestWriteFileMetadataAuto_DropsStaleCorruptedCopy(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	refs := newFakeStoreRefCounter()
	ms.SetStoreRefCounter(refs)

	storeRef := filepath.Join(t.TempDir(), "10767-release.nzbz")
	paths := writeSharedRelease(t, ms, storeRef, "s01e01.mkv")

	ctx := context.Background()
	require.NoError(t, ms.MoveToCorrupted(ctx, paths[0]))
	require.FileExists(t, corruptedMetaPath(root, paths[0]))

	reimported := &metapb.FileMetadata{
		FileSize: 100,
		Status:   metapb.FileStatus_FILE_STATUS_HEALTHY,
		SegmentData: []*metapb.SegmentData{
			{Id: "reimport@n", SegmentSize: 100, StartOffset: 0, EndOffset: 99},
		},
	}
	require.NoError(t, ms.WriteFileMetadataAuto(ctx, paths[0], reimported, nil, ""))

	assert.NoFileExists(t, corruptedMetaPath(root, paths[0]), "a successful re-import must reap its safety copy")
	assert.NoFileExists(t, storeRef, "the copy's store reference must be released")
	assert.False(t, refs.tracked(storeRef))

	got, err := ms.ReadFileMetadata(paths[0])
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(100), got.FileSize)
}

func TestWriteFileMetadataAuto_NoCorruptedCopyIsNoop(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	refs := newFakeStoreRefCounter()
	ms.SetStoreRefCounter(refs)

	meta := &metapb.FileMetadata{
		FileSize: 100,
		Status:   metapb.FileStatus_FILE_STATUS_HEALTHY,
		SegmentData: []*metapb.SegmentData{
			{Id: "fresh@n", SegmentSize: 100, StartOffset: 0, EndOffset: 99},
		},
	}

	ctx := context.Background()
	require.NoError(t, ms.WriteFileMetadataAuto(ctx, "tv/fresh.mkv", meta, nil, ""))

	got, err := ms.ReadFileMetadata("tv/fresh.mkv")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, refs.counts)
	assert.NoDirExists(t, filepath.Join(root, "corrupted_metadata"))
}

func TestCorruptedStats_CountsRetainedCopies(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	ctx := context.Background()
	count, totalBytes, err := ms.CorruptedStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, int64(0), totalBytes)

	storeRef := filepath.Join(t.TempDir(), "10767-release.nzbz")
	paths := writeSharedRelease(t, ms, storeRef, "s01e01.mkv", "s01e02.mkv")
	for _, p := range paths {
		require.NoError(t, ms.MoveToCorrupted(ctx, p))
	}

	count, totalBytes, err = ms.CorruptedStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Positive(t, totalBytes)
}

func TestMoveToCorrupted_RetentionClockStartsAtTheMove(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := "tv/old-import.mkv"
	meta := &metapb.FileMetadata{
		FileSize:   100,
		Status:     metapb.FileStatus_FILE_STATUS_HEALTHY,
		ModifiedAt: time.Now().Add(-365 * 24 * time.Hour).Unix(),
		SegmentData: []*metapb.SegmentData{
			{Id: "old@n", SegmentSize: 100, StartOffset: 0, EndOffset: 99},
		},
	}
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	ctx := context.Background()
	require.NoError(t, ms.MoveToCorrupted(ctx, virtualPath))

	// The .meta's disk mtime tracks ModifiedAt, so without a restamp a year-old
	// import would be pruned the instant it was quarantined.
	removed, err := ms.PruneCorrupted(ctx, 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
	assert.FileExists(t, corruptedMetaPath(root, virtualPath))
}
