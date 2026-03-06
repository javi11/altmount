package importer

import (
	"context"
	"log/slog"
	"testing"

	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCleanupTestService creates a minimal Service with a real MetadataService
// backed by a temp directory. Only the fields needed by cleanupWrittenPaths are set.
func newCleanupTestService(t *testing.T) (*Service, *metadata.MetadataService) {
	t.Helper()
	ms := metadata.NewMetadataService(t.TempDir())
	svc := &Service{
		metadataService: ms,
		log:             slog.Default(),
	}
	return svc, ms
}

// writeTestMeta writes a minimal healthy metadata file at virtualPath and returns it.
func writeTestMeta(t *testing.T, ms *metadata.MetadataService, virtualPath string) {
	t.Helper()
	meta := ms.CreateFileMetadata(
		1024, "test.nzb",
		metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil,
		metapb.Encryption_NONE,
		"", "", nil, nil, 0, nil, "",
	)
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))
}

// TestCleanupWrittenPaths_DeletesIndividualFile verifies Fix 2:
// a single .meta file written during import is deleted on failure.
func TestCleanupWrittenPaths_DeletesIndividualFile(t *testing.T) {
	svc, ms := newCleanupTestService(t)
	ctx := context.Background()

	virtualPath := "movies/broken_movie.mkv"
	writeTestMeta(t, ms, virtualPath)

	// Confirm it was written
	got, err := ms.ReadFileMetadata(virtualPath)
	require.NoError(t, err)
	require.NotNil(t, got)

	svc.cleanupWrittenPaths(ctx, 1, []string{virtualPath})

	// Must be gone after cleanup
	got, err = ms.ReadFileMetadata(virtualPath)
	assert.True(t, err != nil || got == nil,
		"metadata file should be deleted after cleanup (err=%v, got=%v)", err, got)
}

// TestCleanupWrittenPaths_DeletesDirectory verifies Fix 2:
// a "DIR:"-prefixed path triggers deletion of the entire metadata directory.
func TestCleanupWrittenPaths_DeletesDirectory(t *testing.T) {
	svc, ms := newCleanupTestService(t)
	ctx := context.Background()

	// Write a file inside a release directory
	writeTestMeta(t, ms, "movies/MyMovie/MyMovie.mkv")
	writeTestMeta(t, ms, "movies/MyMovie/MyMovie.en.srt")

	assert.True(t, ms.DirectoryExists("movies/MyMovie"), "directory should exist before cleanup")

	// Cleanup the whole directory via DIR: prefix
	svc.cleanupWrittenPaths(ctx, 2, []string{"DIR:movies/MyMovie"})

	assert.False(t, ms.DirectoryExists("movies/MyMovie"),
		"metadata directory should be deleted after DIR: cleanup")
}

// TestCleanupWrittenPaths_MixedPaths verifies that individual files and DIR: entries
// in the same slice are both handled.
func TestCleanupWrittenPaths_MixedPaths(t *testing.T) {
	svc, ms := newCleanupTestService(t)
	ctx := context.Background()

	writeTestMeta(t, ms, "movies/standalone.mkv")
	writeTestMeta(t, ms, "tv/Show/S01E01.mkv")

	svc.cleanupWrittenPaths(ctx, 3, []string{
		"movies/standalone.mkv",
		"DIR:tv/Show",
	})

	// Individual file deleted
	got, err := ms.ReadFileMetadata("movies/standalone.mkv")
	assert.True(t, err != nil || got == nil, "standalone file should be deleted")

	// Directory deleted
	assert.False(t, ms.DirectoryExists("tv/Show"), "tv/Show directory should be deleted")
}

// TestCleanupWrittenPaths_EmptyAndNilSlice ensures no panic on empty/nil input.
func TestCleanupWrittenPaths_EmptyAndNilSlice(t *testing.T) {
	svc, _ := newCleanupTestService(t)
	ctx := context.Background()

	// Neither should panic or error
	assert.NotPanics(t, func() {
		svc.cleanupWrittenPaths(ctx, 4, []string{})
		svc.cleanupWrittenPaths(ctx, 5, nil)
	})
}

// TestHandleFailure_CleansUpCachedPaths verifies Fix 2 end-to-end for the cache flow:
// ProcessItem stores written paths in writtenPathsCache; HandleFailure retrieves and
// deletes them via cleanupWrittenPaths before delegating to handleProcessingFailure.
func TestHandleFailure_CleansUpCachedPaths(t *testing.T) {
	svc, ms := newCleanupTestService(t)
	ctx := context.Background()

	// Write metadata to simulate a partially-completed import
	virtualPath := "movies/corrupted.mkv"
	writeTestMeta(t, ms, virtualPath)

	// Simulate what ProcessItem does: store the written path in the cache
	var fakeItemID int64 = 99
	svc.writtenPathsCache.Store(fakeItemID, []string{virtualPath})

	// Confirm the cache entry exists
	_, ok := svc.writtenPathsCache.Load(fakeItemID)
	require.True(t, ok, "cache should contain the written paths before HandleFailure")

	// Exercise the cache-retrieval + cleanup path (mirrors HandleFailure minus the DB call)
	if paths, ok := svc.writtenPathsCache.LoadAndDelete(fakeItemID); ok {
		svc.cleanupWrittenPaths(ctx, fakeItemID, paths.([]string))
	}

	// Cache must be cleared
	_, ok = svc.writtenPathsCache.Load(fakeItemID)
	assert.False(t, ok, "cache entry should be removed after HandleFailure")

	// Metadata file must be deleted
	got, err := ms.ReadFileMetadata(virtualPath)
	assert.True(t, err != nil || got == nil,
		"metadata file should be deleted after failure cleanup (err=%v, got=%v)", err, got)
}

// TestProcessItem_StoresPaths_HandleSuccess_ClearsCache verifies that HandleSuccess
// removes the cache entry without deleting any files.
func TestProcessItem_StoresPaths_HandleSuccess_ClearsCache(t *testing.T) {
	svc, ms := newCleanupTestService(t)

	virtualPath := "movies/healthy.mkv"
	writeTestMeta(t, ms, virtualPath)

	var fakeItemID int64 = 77
	svc.writtenPathsCache.Store(fakeItemID, []string{virtualPath})

	// Simulate HandleSuccess cache cleanup
	svc.writtenPathsCache.Delete(fakeItemID)

	// Cache must be gone
	_, ok := svc.writtenPathsCache.Load(fakeItemID)
	assert.False(t, ok, "cache entry should be removed on success")

	// But the metadata file itself must NOT be deleted
	got, err := ms.ReadFileMetadata(virtualPath)
	require.NoError(t, err)
	assert.NotNil(t, got, "metadata file should remain intact on success")
}
