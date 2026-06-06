package health

import (
	"context"
	"database/sql"
	"errors"
	"runtime"
	"sync"
	"testing"

	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/arrs/model"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/importer"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	nntppool "github.com/javi11/nntppool/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPoolManager implements pool.Manager and always fails GetPool so segment validation fails.
type mockPoolManager struct{}

func (m *mockPoolManager) GetPool() (pool.NntpClient, error) {
	return nil, errors.New("no pool available (test mock)")
}
func (m *mockPoolManager) SetProviders(_ []nntppool.Provider) error { return nil }
func (m *mockPoolManager) ClearPool() error                         { return nil }
func (m *mockPoolManager) HasPool() bool                            { return false }
func (m *mockPoolManager) GetMetrics() (pool.MetricsSnapshot, error) {
	return pool.MetricsSnapshot{}, nil
}
func (m *mockPoolManager) ResetMetrics(_ context.Context, _, _ bool) error { return nil }
func (m *mockPoolManager) ResetProviderErrors(_ context.Context) error     { return nil }
func (m *mockPoolManager) IncArticlesDownloaded()                          {}
func (m *mockPoolManager) UpdateDownloadProgress(_ string, _ int64)        {}
func (m *mockPoolManager) IncArticlesPosted()                              {}
func (m *mockPoolManager) AddProvider(_ nntppool.Provider) error           { return nil }
func (m *mockPoolManager) RemoveProvider(_ string) error                   { return nil }
func (m *mockPoolManager) ResetProviderQuota(_ context.Context, _ string) error {
	return nil
}
func (m *mockPoolManager) SetProviderIDs(_ map[string]string) {}
func (m *mockPoolManager) AcquireImportSlot(_ context.Context) (func(), error) {
	return func() {}, nil
}
func (m *mockPoolManager) SetAdmissionCaps(_ int, _ int)               {}
func (m *mockPoolManager) SetStreamSource(_ pool.StreamActivitySource) {}
func (m *mockPoolManager) NotifyStreamChange()                         {}

// mockARRsService captures TriggerFileRescan calls and returns a configurable error.
type mockARRsService struct {
	mu        sync.Mutex
	calls     []triggerCall
	returnErr error
}

type triggerCall struct {
	pathForRescan string
	relativePath  string
}

func (m *mockARRsService) TriggerFileRescan(_ context.Context, pathForRescan string, relativePath string, _ *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, triggerCall{pathForRescan: pathForRescan, relativePath: relativePath})
	return m.returnErr
}

func (m *mockARRsService) DiscoverFileMetadata(_ context.Context, _, _, _, _ string) (*model.WebhookMetadata, error) {
	return nil, nil
}

// mockImportService implements importer.ImportService for testing.
type mockImportService struct {
	importer.ImportService
}

func (m *mockImportService) RegenerateMetadata(_ context.Context, _ string) error {
	return nil
}

// repairTestEnv holds all the pieces needed for repair e2e tests.
type repairTestEnv struct {
	db              *sql.DB
	healthRepo      *database.HealthRepository
	metadataService *metadata.MetadataService
	healthChecker   *HealthChecker
	mockARRs        *mockARRsService
	hw              *HealthWorker
}

func newRepairTestEnv(t *testing.T, tempDir string, arrsErr error) *repairTestEnv {
	t.Helper()

	db, err := sql.Open("sqlite3", "file::memory:?cache=shared&mode=memory")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS file_health (
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
			priority INTEGER NOT NULL DEFAULT 0,
			streaming_failure_count INTEGER DEFAULT 0,
			is_masked BOOLEAN DEFAULT FALSE,
			indexer TEXT DEFAULT NULL
		);

		CREATE TABLE IF NOT EXISTS system_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)

	healthRepo := database.NewHealthRepository(db, database.DialectSQLite)
	metadataService := metadata.NewMetadataService(tempDir)

	healthEnabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &healthEnabled
	cfg.Health.MaxRetries = 3
	cfg.Metadata.RootPath = tempDir
	cfg.MountPath = "/mnt/test"
	cfg.Health.MaxConcurrentJobs = 1
	cfg.Health.CheckIntervalSeconds = 3600
	cfg.Health.SegmentSamplePercentage = 10
	cfg.Health.MaxConnectionsForHealthChecks = 1

	configManager := config.NewManager(cfg, "")

	mockARRs := &mockARRsService{returnErr: arrsErr}
	mockImporter := &mockImportService{}

	healthChecker := NewHealthChecker(
		healthRepo,
		metadataService,
		&mockPoolManager{},
		configManager.GetConfig,
		&MockRcloneClient{},
	)

	hw := NewHealthWorker(
		healthChecker,
		healthRepo,
		metadataService,
		mockARRs,
		mockImporter,
		configManager.GetConfig,
		nil,
	)

	return &repairTestEnv{
		db:              db,
		healthRepo:      healthRepo,
		metadataService: metadataService,
		healthChecker:   healthChecker,
		mockARRs:        mockARRs,
		hw:              hw,
	}
}

// validSegmentMeta creates a FileMetadata with one segment that covers the full fileSize,
// so CheckMetadataIntegrity passes. Pool failure then causes EventTypeCheckFailed.
func validSegmentMeta(ms *metadata.MetadataService, fileSize int64) *metapb.FileMetadata {
	seg := &metapb.SegmentData{
		Id:          "test-article-001@test.example.com",
		SegmentSize: fileSize,
		StartOffset: 0,
		EndOffset:   fileSize - 1,
	}
	return ms.CreateFileMetadata(
		fileSize, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		[]*metapb.SegmentData{seg},
		metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
}

// insertFileHealth directly inserts a file_health row with the given parameters.
func insertFileHealth(t *testing.T, db *sql.DB, filePath, libraryPath string, retryCount, maxRetries int) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO file_health
			(file_path, library_path, status, retry_count, max_retries,
			 repair_retry_count, max_repair_retries, scheduled_check_at)
		VALUES (?, ?, 'pending', ?, ?, 0, 3, datetime('now', '-1 second'))
	`, filePath, libraryPath, retryCount, maxRetries)
	require.NoError(t, err)
}

// advanceScheduledCheck sets scheduled_check_at to the past so the next cycle picks it up.
func advanceScheduledCheck(t *testing.T, db *sql.DB, filePath string) {
	t.Helper()
	_, err := db.Exec(
		`UPDATE file_health SET scheduled_check_at = datetime('now', '-1 second') WHERE file_path = ?`,
		filePath,
	)
	require.NoError(t, err)
}

// TestE2E_FileRepairTriggered_ARRResearchCalled verifies that when a file's retry_count
// is already at MaxRetries-1, a single health check cycle triggers ARR repair,
// moves metadata to the corrupted folder, and sets DB status to repair_triggered.
func TestE2E_FileRepairTriggered_ARRResearchCalled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	tempDir := t.TempDir()
	env := newRepairTestEnv(t, tempDir, nil) // ARR returns nil (success)

	ctx := context.Background()
	filePath := "series/show.s01e01.mkv"
	libraryPath := "/media/library/show.s01e01.mkv"
	maxRetries := 3

	// Write valid metadata so the checker can read it.
	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))

	// Insert file already at last retry (retry_count = maxRetries-1 = 2).
	insertFileHealth(t, env.db, filePath, libraryPath, maxRetries-1, maxRetries)

	// Run one cycle — this should exhaust retries and call triggerFileRepair.
	require.NoError(t, env.hw.runHealthCheckCycle(ctx))

	// ARR should have been called exactly once with the library path.
	env.mockARRs.mu.Lock()
	calls := env.mockARRs.calls
	env.mockARRs.mu.Unlock()

	require.Len(t, calls, 1, "expected exactly one TriggerFileRescan call")
	assert.Equal(t, libraryPath, calls[0].pathForRescan, "pathForRescan should be the library_path")

	// DB status should be repair_triggered.
	fh, err := env.healthRepo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fh)
	assert.Equal(t, database.HealthStatusRepairTriggered, fh.Status)

	// Metadata should have been moved to the corrupted folder (original path no longer readable).
	original, readErr := env.metadataService.ReadFileMetadata(filePath)
	assert.Nil(t, original, "metadata should not be readable at original path after move")
	assert.NoError(t, readErr, "ReadFileMetadata should return (nil, nil) for missing file")
}

// TestE2E_FileRepairTriggered_FullRetryFlow verifies that a file starting at retry_count=0
// requires exactly MaxRetries failed cycles before ARR repair is triggered.
func TestE2E_FileRepairTriggered_FullRetryFlow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	tempDir := t.TempDir()
	env := newRepairTestEnv(t, tempDir, nil) // ARR returns nil (success)

	ctx := context.Background()
	filePath := "series/show.s01e02.mkv"
	libraryPath := "/media/library/show.s01e02.mkv"
	maxRetries := 3

	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))
	insertFileHealth(t, env.db, filePath, libraryPath, 0, maxRetries)

	// Cycle 1: retry_count goes from 0 → 1, ARR not called yet.
	require.NoError(t, env.hw.runHealthCheckCycle(ctx))
	fh, err := env.healthRepo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fh)
	assert.Equal(t, database.HealthStatusPending, fh.Status, "should still be pending after cycle 1")
	assert.Equal(t, 1, fh.RetryCount, "retry_count should be 1 after cycle 1")
	assert.Empty(t, env.mockARRs.calls, "ARR should not be called after cycle 1")

	// Advance scheduled_check_at so cycle 2 picks the file up.
	advanceScheduledCheck(t, env.db, filePath)

	// Cycle 2: retry_count goes from 1 → 2, ARR not called yet.
	require.NoError(t, env.hw.runHealthCheckCycle(ctx))
	fh, err = env.healthRepo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fh)
	assert.Equal(t, database.HealthStatusPending, fh.Status, "should still be pending after cycle 2")
	assert.Equal(t, 2, fh.RetryCount, "retry_count should be 2 after cycle 2")
	assert.Empty(t, env.mockARRs.calls, "ARR should not be called after cycle 2")

	// Advance again for cycle 3.
	advanceScheduledCheck(t, env.db, filePath)

	// Cycle 3: retry_count=2 == maxRetries-1, so repair is triggered.
	require.NoError(t, env.hw.runHealthCheckCycle(ctx))

	env.mockARRs.mu.Lock()
	calls := env.mockARRs.calls
	env.mockARRs.mu.Unlock()

	require.Len(t, calls, 1, "expected exactly one TriggerFileRescan call after cycle 3")
	assert.Equal(t, libraryPath, calls[0].pathForRescan)

	fh, err = env.healthRepo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fh)
	assert.Equal(t, database.HealthStatusRepairTriggered, fh.Status)

	// Metadata moved to corrupted folder.
	original, readErr := env.metadataService.ReadFileMetadata(filePath)
	assert.Nil(t, original)
	assert.NoError(t, readErr)
}

// TestE2E_RepairBudgetExhausted_PendingFileMarkedCorrupted verifies the terminal state:
// a pending file that fails its last health retry while its repair budget is already
// spent (repair_retry_count == max_repair_retries, preserved across re-import upserts
// and webhook relinks) is marked corrupted instead of triggering yet another ARR rescan.
// Without this guard, a genuinely dead release loops forever: rescan → re-download →
// re-import resets to pending → check fails → rescan...
func TestE2E_RepairBudgetExhausted_PendingFileMarkedCorrupted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	tempDir := t.TempDir()
	env := newRepairTestEnv(t, tempDir, nil)

	ctx := context.Background()
	filePath := "series/show.s01e05.mkv"
	libraryPath := "/media/library/show.s01e05.mkv"
	maxRetries := 3

	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))

	// Pending file at its last health retry, with the repair budget already spent.
	insertFileHealth(t, env.db, filePath, libraryPath, maxRetries-1, maxRetries)
	_, err := env.db.Exec(
		`UPDATE file_health SET repair_retry_count = 3, max_repair_retries = 3 WHERE file_path = ?`,
		filePath,
	)
	require.NoError(t, err)

	require.NoError(t, env.hw.runHealthCheckCycle(ctx))

	// ARR must NOT be called — no more re-downloads for this title.
	env.mockARRs.mu.Lock()
	callCount := len(env.mockARRs.calls)
	env.mockARRs.mu.Unlock()
	assert.Equal(t, 0, callCount, "exhausted repair budget must not trigger another ARR rescan")

	// Status must be terminal corrupted.
	fh, err := env.healthRepo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fh)
	assert.Equal(t, database.HealthStatusCorrupted, fh.Status)

	// Metadata hidden in the safety folder (this path follows a real failed check).
	original, readErr := env.metadataService.ReadFileMetadata(filePath)
	assert.Nil(t, original, "metadata should be moved to the safety folder")
	assert.NoError(t, readErr)
}

// TestE2E_ExhaustedRepairTriggered_SweptToCorrupted verifies that records already in
// repair_triggered with a spent repair budget are picked up by the repair-notification
// pass and finalized as corrupted (previously they were filtered out of every query and
// stuck in repair_triggered forever). The sweep must be non-destructive: no ARR call and
// no metadata move, since no fresh check has failed.
func TestE2E_ExhaustedRepairTriggered_SweptToCorrupted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	tempDir := t.TempDir()
	env := newRepairTestEnv(t, tempDir, nil)

	ctx := context.Background()
	filePath := "series/show.s01e06.mkv"
	libraryPath := "/media/library/show.s01e06.mkv"

	// Metadata exists (e.g. a re-import landed after the last repair re-trigger).
	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))

	_, err := env.db.Exec(`
		INSERT INTO file_health
			(file_path, library_path, status, retry_count, max_retries,
			 repair_retry_count, max_repair_retries, scheduled_check_at)
		VALUES (?, ?, 'repair_triggered', 2, 3, 3, 3, datetime('now', '-1 second'))
	`, filePath, libraryPath)
	require.NoError(t, err)

	require.NoError(t, env.hw.runHealthCheckCycle(ctx))

	env.mockARRs.mu.Lock()
	callCount := len(env.mockARRs.calls)
	env.mockARRs.mu.Unlock()
	assert.Equal(t, 0, callCount, "sweep must not trigger ARR rescans")

	fh, err := env.healthRepo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fh)
	assert.Equal(t, database.HealthStatusCorrupted, fh.Status,
		"exhausted repair_triggered record must be finalized as corrupted, not stuck")

	// Metadata must be left in place — the sweep has not re-validated content.
	original, readErr := env.metadataService.ReadFileMetadata(filePath)
	require.NoError(t, readErr)
	assert.NotNil(t, original, "sweep must not move metadata of a possibly-good copy")
}

// TestE2E_FileRepairTriggered_ARRReturnsAlreadySatisfied verifies that when ARR returns
// ErrEpisodeAlreadySatisfied the health record is deleted (zombie cleanup).
func TestE2E_FileRepairTriggered_ARRReturnsAlreadySatisfied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	tempDir := t.TempDir()
	env := newRepairTestEnv(t, tempDir, arrs.ErrEpisodeAlreadySatisfied)

	ctx := context.Background()
	filePath := "series/show.s01e03.mkv"
	libraryPath := "/media/library/show.s01e03.mkv"
	maxRetries := 3

	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))
	insertFileHealth(t, env.db, filePath, libraryPath, maxRetries-1, maxRetries)

	require.NoError(t, env.hw.runHealthCheckCycle(ctx))

	// ARR was called.
	env.mockARRs.mu.Lock()
	callCount := len(env.mockARRs.calls)
	env.mockARRs.mu.Unlock()
	assert.Equal(t, 1, callCount)

	// Health record must be deleted, not set to repair_triggered.
	fh, err := env.healthRepo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	assert.Nil(t, fh, "health record should be deleted when ARR returns ErrEpisodeAlreadySatisfied")
}

// TestE2E_FileRepairTriggered_ARRReturnsPathNotFound verifies that when ARR returns
// ErrPathMatchFailed (an ambiguous path miss — e.g. an ARR-renamed/reorganized library)
// the file is NOT destroyed: the health record is kept (marked corrupted) and its metadata
// is preserved, so the user's library symlink and the underlying virtual file survive.
// Genuine orphans are removed separately by the guarded library-sync orphan pass.
func TestE2E_FileRepairTriggered_ARRReturnsPathNotFound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	tempDir := t.TempDir()
	env := newRepairTestEnv(t, tempDir, arrs.ErrPathMatchFailed)

	ctx := context.Background()
	filePath := "series/show.s01e04.mkv"
	libraryPath := "/media/library/show.s01e04.mkv"
	maxRetries := 3

	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))
	insertFileHealth(t, env.db, filePath, libraryPath, maxRetries-1, maxRetries)

	require.NoError(t, env.hw.runHealthCheckCycle(ctx))

	// ARR was called.
	env.mockARRs.mu.Lock()
	callCount := len(env.mockARRs.calls)
	env.mockARRs.mu.Unlock()
	assert.Equal(t, 1, callCount)

	// Health record must be preserved (not deleted) and marked corrupted: a path-match miss
	// is not a reliable orphan signal, so the file must never be destroyed on this path.
	fh, err := env.healthRepo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fh, "health record must NOT be deleted when ARR returns ErrPathMatchFailed")
	assert.Equal(t, database.HealthStatusCorrupted, fh.Status)

	// Metadata must be preserved (not deleted) so the underlying file/symlink survives.
	original, readErr := env.metadataService.ReadFileMetadata(filePath)
	require.NoError(t, readErr)
	assert.NotNil(t, original, "metadata must be preserved when ARR returns ErrPathMatchFailed")
}
