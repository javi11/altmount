package nzbfilesystem

import (
	"context"
	"database/sql"
	"errors"
	"runtime"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/usenet"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupStreamHealthEnv builds an in-memory health repository and a metadata service rooted
// in a temp dir, so the streaming-failure handler (updateFileHealthOnError) can be exercised
// end-to-end against real persistence.
func setupStreamHealthEnv(t *testing.T) (*database.HealthRepository, *sql.DB, *metadata.MetadataService) {
	t.Helper()

	db, err := sql.Open("sqlite3", "file::memory:?cache=shared&mode=memory")
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
			indexer TEXT DEFAULT NULL,
			download_id TEXT DEFAULT NULL
		);
	`)
	require.NoError(t, err)

	return database.NewHealthRepository(db, database.DialectSQLite), db, metadata.NewMetadataService(t.TempDir())
}

// newStreamFailureMVF wires a MetadataVirtualFile to the given real services with a nil
// repairCoalescer (ShouldTrigger returns true, EnqueueRefresh is a no-op).
func newStreamFailureMVF(ctx context.Context, name string, repo *database.HealthRepository, ms *metadata.MetadataService, seg []*metapb.SegmentData, cfg *config.Config) *MetadataVirtualFile {
	return &MetadataVirtualFile{
		name:             name,
		ctx:              ctx,
		meta:             &fileHandleMeta{FileSize: 1024, SegmentData: seg},
		metadataService:  ms,
		healthRepository: repo,
		configGetter:     func() *config.Config { return cfg },
	}
}

func writeStreamMeta(t *testing.T, ms *metadata.MetadataService, filePath string) []*metapb.SegmentData {
	t.Helper()
	meta := ms.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		[]*metapb.SegmentData{{Id: "a@b.example.com", SegmentSize: 1024, StartOffset: 0, EndOffset: 1023}},
		metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	require.NoError(t, ms.WriteFileMetadata(filePath, meta))
	return meta.SegmentData
}

// TestUpdateFileHealthOnError_HealthDisabled_NoRepair pins the bug fix: a mid-stream
// DataCorruptionError while the health system is disabled records the file as corrupted for
// visibility but does NOT trigger a repair (no repair_triggered status, no metadata move).
func TestUpdateFileHealthOnError_HealthDisabled_NoRepair(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	repo, db, ms := setupStreamHealthEnv(t)
	ctx := context.Background()

	filePath := "series/stream.s01e01.mkv"
	libraryPath := "/media/library/stream.s01e01.mkv"
	seg := writeStreamMeta(t, ms, filePath)

	// Pre-existing imported health record (with library_path) — the enabled path would move it.
	_, err := db.Exec(
		`INSERT INTO file_health (file_path, library_path, status, scheduled_check_at) VALUES (?, ?, 'healthy', datetime('now'))`,
		filePath, libraryPath,
	)
	require.NoError(t, err)

	disabled := false
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &disabled
	cfg.MountPath = ""

	mvf := newStreamFailureMVF(ctx, filePath, repo, ms, seg, cfg)
	mvf.updateFileHealthOnError(&usenet.DataCorruptionError{UnderlyingErr: errors.New("article not found"), NoRetry: true}, true)

	fh, err := repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fh)
	assert.Equal(t, database.HealthStatusCorrupted, fh.Status,
		"health disabled must record corrupted, not repair_triggered")

	original, readErr := ms.ReadFileMetadata(filePath)
	require.NoError(t, readErr)
	assert.NotNil(t, original, "health disabled must not move metadata to the corrupted folder")
}

// TestUpdateFileHealthOnError_HealthEnabled_TriggersRepair guards the unchanged behavior:
// with the health system enabled, a mid-stream failure triggers the repair (repair_triggered
// status + metadata moved to the corrupted folder).
func TestUpdateFileHealthOnError_HealthEnabled_TriggersRepair(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	repo, db, ms := setupStreamHealthEnv(t)
	ctx := context.Background()

	filePath := "series/stream.s01e02.mkv"
	libraryPath := "/media/library/stream.s01e02.mkv"
	seg := writeStreamMeta(t, ms, filePath)

	_, err := db.Exec(
		`INSERT INTO file_health (file_path, library_path, status, scheduled_check_at) VALUES (?, ?, 'healthy', datetime('now'))`,
		filePath, libraryPath,
	)
	require.NoError(t, err)

	enabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &enabled
	cfg.MountPath = ""

	mvf := newStreamFailureMVF(ctx, filePath, repo, ms, seg, cfg)
	mvf.updateFileHealthOnError(&usenet.DataCorruptionError{UnderlyingErr: errors.New("article not found"), NoRetry: true}, true)

	fh, err := repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fh)
	assert.Equal(t, database.HealthStatusRepairTriggered, fh.Status,
		"health enabled must trigger repair")

	original, readErr := ms.ReadFileMetadata(filePath)
	require.NoError(t, readErr)
	assert.Nil(t, original, "health enabled must move metadata to the corrupted folder")
}

// TestUpdateFileHealthOnError_FailureMasking_MasksRepair verifies that when failure masking
// is enabled, a streaming failure below the threshold does not immediately trigger a repair
// or move the metadata, but increments the failure count instead.
func TestUpdateFileHealthOnError_FailureMasking_MasksRepair(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	repo, db, ms := setupStreamHealthEnv(t)
	ctx := context.Background()

	filePath := "series/stream.s01e03.mkv"
	libraryPath := "/media/library/stream.s01e03.mkv"
	seg := writeStreamMeta(t, ms, filePath)

	_, err := db.Exec(
		`INSERT INTO file_health (file_path, library_path, status, scheduled_check_at, streaming_failure_count) VALUES (?, ?, 'healthy', datetime('now'), 0)`,
		filePath, libraryPath,
	)
	require.NoError(t, err)

	healthEnabled := true
	maskingEnabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &healthEnabled
	cfg.Streaming.FailureMasking.Enabled = &maskingEnabled
	cfg.Streaming.FailureMasking.Threshold = 3
	cfg.MountPath = ""

	mvf := newStreamFailureMVF(ctx, filePath, repo, ms, seg, cfg)

	// First failure: below threshold of 3
	mvf.updateFileHealthOnError(&usenet.DataCorruptionError{UnderlyingErr: errors.New("article not found"), NoRetry: true}, true)

	fh, err := repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fh)
	assert.Equal(t, database.HealthStatusPending, fh.Status, "should be pending, not repair_triggered yet")
	assert.Equal(t, 1, fh.StreamingFailureCount, "failure count should be incremented to 1")

	original, readErr := ms.ReadFileMetadata(filePath)
	require.NoError(t, readErr)
	assert.NotNil(t, original, "should NOT move metadata to corrupted folder yet")

	// Second failure: still below threshold
	mvf.updateFileHealthOnError(&usenet.DataCorruptionError{UnderlyingErr: errors.New("article not found"), NoRetry: true}, true)

	fh, err = repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	assert.Equal(t, 2, fh.StreamingFailureCount, "failure count should be 2")
	original, _ = ms.ReadFileMetadata(filePath)
	assert.NotNil(t, original, "should still not move metadata")

	// Third failure: meets threshold (3) -> triggers repair
	mvf.updateFileHealthOnError(&usenet.DataCorruptionError{UnderlyingErr: errors.New("article not found"), NoRetry: true}, true)

	fh, err = repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	assert.Equal(t, database.HealthStatusRepairTriggered, fh.Status, "should trigger repair now")
	assert.Equal(t, 3, fh.StreamingFailureCount, "failure count should be 3")

	original, _ = ms.ReadFileMetadata(filePath)
	assert.Nil(t, original, "metadata should be moved to corrupted folder now")
}

