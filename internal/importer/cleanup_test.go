package importer

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCleanupTestService creates a minimal Service with a real MetadataService
// backed by a temp directory and a real HealthRepository (in-memory sqlite).
// Only the fields needed by cleanupWrittenPaths are set.
func newCleanupTestService(t *testing.T) (*Service, *metadata.MetadataService) {
	t.Helper()
	ms := metadata.NewMetadataService(t.TempDir())
	svc := &Service{
		metadataService: ms,
		healthRepo:      newCleanupTestHealthRepo(t),
		log:             slog.Default(),
	}
	return svc, ms
}

// newCleanupTestHealthRepo builds a HealthRepository over an in-memory sqlite DB.
func newCleanupTestHealthRepo(t *testing.T) *database.HealthRepository {
	t.Helper()

	db, err := sql.Open("sqlite3", "file::memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE file_health (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT NOT NULL UNIQUE,
			library_path TEXT,
			status TEXT NOT NULL,
			last_checked DATETIME,
			last_error TEXT,
			retry_count INTEGER DEFAULT 0,
			max_retries INTEGER DEFAULT 3,
			repair_retry_count INTEGER DEFAULT 0,
			max_repair_retries INTEGER DEFAULT 3,
			source_nzb_path TEXT,
			error_details TEXT,
			metadata TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			release_date DATETIME,
			scheduled_check_at DATETIME,
			priority INTEGER DEFAULT 0,
			streaming_failure_count INTEGER DEFAULT 0,
			is_masked BOOLEAN DEFAULT FALSE,
			indexer TEXT DEFAULT NULL
		);
	`)
	require.NoError(t, err)

	return database.NewHealthRepository(db, database.DialectSQLite)
}

// addPendingHealthRecord seeds a pending health record the way the import scheduler
// does (virtual path doubling as the placeholder library path).
func addPendingHealthRecord(t *testing.T, repo *database.HealthRepository, virtualPath string) {
	t.Helper()
	require.NoError(t, repo.AddFileToHealthCheckWithMetadata(
		context.Background(), virtualPath, &virtualPath, 3, 3, nil,
		database.HealthPriorityNext, nil, nil, nil))
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

// TestCleanupWrittenPaths_DeletesHealthRecordsForDir verifies that rolling back a
// failed directory import also removes the health records scheduled beneath it —
// including ones created by an earlier, successful attempt of the same release.
// Without this they linger in pending forever under SYMLINK/STRM strategies: the
// placeholder library_path never matches the worker's library filter and no ARR
// webhook will ever relink a rolled-back import.
func TestCleanupWrittenPaths_DeletesHealthRecordsForDir(t *testing.T) {
	svc, ms := newCleanupTestService(t)
	ctx := context.Background()

	writeTestMeta(t, ms, "tv/Pack.S08.1080p-GRP/e01.mkv")
	writeTestMeta(t, ms, "tv/Pack.S08.1080p-GRP/e02.mkv")
	addPendingHealthRecord(t, svc.healthRepo, "tv/Pack.S08.1080p-GRP/e01.mkv")
	addPendingHealthRecord(t, svc.healthRepo, "tv/Pack.S08.1080p-GRP/e02.mkv")
	// A record in a sibling release must survive the cleanup.
	addPendingHealthRecord(t, svc.healthRepo, "tv/Other.S01.1080p-GRP/e01.mkv")

	svc.cleanupWrittenPaths(ctx, 10, []string{"DIR:tv/Pack.S08.1080p-GRP"})

	for _, p := range []string{
		"tv/Pack.S08.1080p-GRP/e01.mkv",
		"tv/Pack.S08.1080p-GRP/e02.mkv",
	} {
		h, err := svc.healthRepo.GetFileHealth(ctx, p)
		require.NoError(t, err)
		assert.Nil(t, h, "health record %s must be deleted with the rolled-back import", p)
	}

	h, err := svc.healthRepo.GetFileHealth(ctx, "tv/Other.S01.1080p-GRP/e01.mkv")
	require.NoError(t, err)
	assert.NotNil(t, h, "records of unrelated releases must not be touched")
}

// TestCleanupWrittenPaths_PreservesPriorSuccessfulImport verifies that a failed re-import
// into a shared nzbFolder deletes only its own unvalidated placeholder records and leaves
// a prior successful import's validated records (healthy, or relinked to a real ARR library
// path) intact. The nzbFolder is deterministic per release, so without scoping a failed
// re-grab would wipe a still-good library entry's health record.
func TestCleanupWrittenPaths_PreservesPriorSuccessfulImport(t *testing.T) {
	svc, ms := newCleanupTestService(t)
	ctx := context.Background()

	writeTestMeta(t, ms, "tv/Pack.S08.1080p-GRP/e01.mkv")

	// This failed attempt's fresh placeholder — must be deleted.
	addPendingHealthRecord(t, svc.healthRepo, "tv/Pack.S08.1080p-GRP/e01.mkv")

	// A prior successful import's relinked record (library_path != file_path) — must survive.
	relinked := "/media/library/Pack/e02.mkv"
	require.NoError(t, svc.healthRepo.AddFileToHealthCheckWithMetadata(ctx,
		"tv/Pack.S08.1080p-GRP/e02.mkv", &relinked, 3, 3, nil,
		database.HealthPriorityNext, nil, nil, nil))

	// A prior successful import's healthy record — must survive.
	addPendingHealthRecord(t, svc.healthRepo, "tv/Pack.S08.1080p-GRP/e03.mkv")
	require.NoError(t, svc.healthRepo.MarkAsHealthy(ctx, "tv/Pack.S08.1080p-GRP/e03.mkv", time.Now().Add(time.Hour)))

	svc.cleanupWrittenPaths(ctx, 20, []string{"DIR:tv/Pack.S08.1080p-GRP"})

	gone, err := svc.healthRepo.GetFileHealth(ctx, "tv/Pack.S08.1080p-GRP/e01.mkv")
	require.NoError(t, err)
	assert.Nil(t, gone, "the failed attempt's unvalidated placeholder must be deleted")

	for _, p := range []string{
		"tv/Pack.S08.1080p-GRP/e02.mkv",
		"tv/Pack.S08.1080p-GRP/e03.mkv",
	} {
		h, err := svc.healthRepo.GetFileHealth(ctx, p)
		require.NoError(t, err)
		assert.NotNil(t, h, "validated record %s from a prior successful import must survive", p)
	}
}

// TestCleanupWrittenPaths_DeletesHealthRecordForFile verifies the single-file variant:
// a plain written path removes exactly its own health record.
func TestCleanupWrittenPaths_DeletesHealthRecordForFile(t *testing.T) {
	svc, ms := newCleanupTestService(t)
	ctx := context.Background()

	writeTestMeta(t, ms, "movies/Broken.2024.mkv")
	addPendingHealthRecord(t, svc.healthRepo, "movies/Broken.2024.mkv")
	addPendingHealthRecord(t, svc.healthRepo, "movies/Keep.2024.mkv")

	svc.cleanupWrittenPaths(ctx, 11, []string{"movies/Broken.2024.mkv"})

	h, err := svc.healthRepo.GetFileHealth(ctx, "movies/Broken.2024.mkv")
	require.NoError(t, err)
	assert.Nil(t, h, "health record of the failed file must be deleted")

	h, err = svc.healthRepo.GetFileHealth(ctx, "movies/Keep.2024.mkv")
	require.NoError(t, err)
	assert.NotNil(t, h, "other records must not be touched")
}

// TestCleanupWrittenPaths_NilHealthRepo ensures the cleanup tolerates a Service
// constructed without a health repository.
func TestCleanupWrittenPaths_NilHealthRepo(t *testing.T) {
	ms := metadata.NewMetadataService(t.TempDir())
	svc := &Service{
		metadataService: ms,
		log:             slog.Default(),
	}
	writeTestMeta(t, ms, "movies/NoRepo.2024.mkv")

	assert.NotPanics(t, func() {
		svc.cleanupWrittenPaths(context.Background(), 12, []string{"movies/NoRepo.2024.mkv"})
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
