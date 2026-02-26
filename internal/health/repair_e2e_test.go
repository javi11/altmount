package health

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	nntppool "github.com/javi11/nntppool/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPoolManager implements pool.Manager and always fails GetPool so segment validation fails.
type mockPoolManager struct{}

func (m *mockPoolManager) GetPool() (*nntppool.Client, error) {
	return nil, errors.New("no pool available (test mock)")
}
func (m *mockPoolManager) SetProviders(_ []nntppool.Provider) error  { return nil }
func (m *mockPoolManager) ClearPool() error                          { return nil }
func (m *mockPoolManager) HasPool() bool                             { return false }
func (m *mockPoolManager) GetMetrics() (pool.MetricsSnapshot, error) { return pool.MetricsSnapshot{}, nil }
func (m *mockPoolManager) ResetMetrics(_ context.Context) error      { return nil }
func (m *mockPoolManager) IncArticlesDownloaded()                    {}
func (m *mockPoolManager) UpdateDownloadProgress(_ string, _ int64)  {}
func (m *mockPoolManager) IncArticlesPosted()                        {}
func (m *mockPoolManager) AddProvider(_ nntppool.Provider) error     { return nil }
func (m *mockPoolManager) RemoveProvider(_ string) error             { return nil }

// mockARRsService captures TriggerFileRescan calls and returns a configurable error.
type mockARRsService struct {
	mu       sync.Mutex
	calls    []triggerCall
	returnErr error
}

type triggerCall struct {
	pathForRescan string
	relativePath  string
}

func (m *mockARRsService) TriggerFileRescan(_ context.Context, pathForRescan string, relativePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, triggerCall{pathForRescan: pathForRescan, relativePath: relativePath})
	return m.returnErr
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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			release_date DATETIME,
			scheduled_check_at DATETIME,
			priority INTEGER NOT NULL DEFAULT 0
		);
	`)
	require.NoError(t, err)

	healthRepo := database.NewHealthRepository(db)
	metadataService := metadata.NewMetadataService(tempDir)

	healthEnabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &healthEnabled
	cfg.Metadata.RootPath = tempDir
	cfg.MountPath = "/mnt/test"
	cfg.Health.MaxConcurrentJobs = 1
	cfg.Health.CheckIntervalSeconds = 3600
	cfg.Health.SegmentSamplePercentage = 10
	verifyData := false
	cfg.Health.VerifyData = &verifyData
	cfg.Health.MaxConnectionsForHealthChecks = 1

	configManager := config.NewManager(cfg, "")

	mockARRs := &mockARRsService{returnErr: arrsErr}

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
		configManager.GetConfig,
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

// TestE2E_FileRepairTriggered_ARRReturnsAlreadySatisfied verifies that when ARR returns
// ErrEpisodeAlreadySatisfied the health record is deleted (zombie cleanup).
func TestE2E_FileRepairTriggered_ARRReturnsAlreadySatisfied(t *testing.T) {
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
// ErrPathMatchFailed the health record is deleted (orphan cleanup).
func TestE2E_FileRepairTriggered_ARRReturnsPathNotFound(t *testing.T) {
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

	// Health record must be deleted, not set to repair_triggered.
	fh, err := env.healthRepo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	assert.Nil(t, fh, "health record should be deleted when ARR returns ErrPathMatchFailed")
}
