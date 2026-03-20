package health

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newResilienceDB creates an in-memory SQLite database with the required schema.
func newResilienceDB(t *testing.T) *sql.DB {
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
			priority INTEGER NOT NULL DEFAULT 0,
			streaming_failure_count INTEGER DEFAULT 0,
			is_masked BOOLEAN DEFAULT FALSE
		);

		CREATE TABLE IF NOT EXISTS system_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)
	return db
}

func TestCleanupZombieRecord_DeletesLibrarySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}

	tempDir := t.TempDir()
	db := newResilienceDB(t)
	healthRepo := database.NewHealthRepository(db, database.DialectSQLite)
	metadataService := metadata.NewMetadataService(tempDir)

	// Create metadata file for the zombie
	virtualPath := filepath.Join("movies", "zombie.mkv")
	meta := metadataService.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	require.NoError(t, metadataService.WriteFileMetadata(virtualPath, meta))

	// Create a temp file to act as the library symlink target
	libraryFile := filepath.Join(tempDir, "library", "zombie.mkv")
	require.NoError(t, os.MkdirAll(filepath.Dir(libraryFile), 0755))
	require.NoError(t, os.WriteFile(libraryFile, []byte("dummy"), 0644))

	// Insert health record with library_path
	// DeleteHealthRecord trims leading "/" so store without it for consistency
	mountPath := "/mnt/test"
	filePath := "mnt/test/" + virtualPath
	_, err := db.Exec(`
		INSERT INTO file_health (file_path, library_path, status, retry_count, max_retries, repair_retry_count, max_repair_retries)
		VALUES (?, ?, 'pending', 0, 3, 0, 3)
	`, filePath, libraryFile)
	require.NoError(t, err)

	// Build HealthWorker
	healthEnabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &healthEnabled
	cfg.Metadata.RootPath = tempDir
	cfg.MountPath = mountPath
	configManager := config.NewManager(cfg, "")

	hw := NewHealthWorker(
		nil, // healthChecker not needed
		healthRepo,
		metadataService,
		&mockARRsService{},
		&mockImportService{},
		configManager.GetConfig,
		nil,
	)

	// Build FileHealth item (FilePath as stored in DB, with mount prefix)
	item := &database.FileHealth{
		FilePath:    "/" + filePath, // cleanupZombieRecord receives full path
		LibraryPath: &libraryFile,
	}

	ctx := context.Background()
	hw.cleanupZombieRecord(ctx, item)

	// Library file should be deleted
	_, err = os.Stat(libraryFile)
	assert.True(t, os.IsNotExist(err), "library file should be deleted by zombie cleanup")

	// Health record should be deleted
	count, err := healthRepo.CountHealthItems(ctx, nil, nil, "")
	require.NoError(t, err)
	assert.Equal(t, 0, count, "health record should be deleted")

	// Metadata should be deleted
	assert.False(t, metadataService.FileExists(virtualPath), "metadata should be deleted")
}

func TestCleanupZombieRecord_NoLibraryPath(t *testing.T) {
	tempDir := t.TempDir()
	db := newResilienceDB(t)
	healthRepo := database.NewHealthRepository(db, database.DialectSQLite)
	metadataService := metadata.NewMetadataService(tempDir)

	mountPath := "/mnt/test"
	filePath := "mnt/test/movies/orphan.mkv" // stored without leading slash (repo trims it)

	// Insert health record without library_path
	_, err := db.Exec(`
		INSERT INTO file_health (file_path, status, retry_count, max_retries, repair_retry_count, max_repair_retries)
		VALUES (?, 'pending', 0, 3, 0, 3)
	`, filePath)
	require.NoError(t, err)

	healthEnabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &healthEnabled
	cfg.Metadata.RootPath = tempDir
	cfg.MountPath = mountPath
	configManager := config.NewManager(cfg, "")

	hw := NewHealthWorker(
		nil,
		healthRepo,
		metadataService,
		&mockARRsService{},
		&mockImportService{},
		configManager.GetConfig,
		nil,
	)

	item := &database.FileHealth{
		FilePath:    "/" + filePath, // cleanupZombieRecord receives the full path with leading /
		LibraryPath: nil,            // No library path
	}

	ctx := context.Background()
	// Should not panic
	hw.cleanupZombieRecord(ctx, item)

	// Health record should be deleted
	count, err := healthRepo.CountHealthItems(ctx, nil, nil, "")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestTwoPassMetadataDelete(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}

	tempDir := t.TempDir()
	db := newResilienceDB(t)
	healthRepo := database.NewHealthRepository(db, database.DialectSQLite)
	metadataService := metadata.NewMetadataService(tempDir)

	// Create metadata files — 5 with library entries, 1 orphan (≈17% < 20% ratio threshold)
	allFiles := []string{"movie_0.mkv", "movie_1.mkv", "movie_2.mkv", "movie_3.mkv", "movie_4.mkv", "orphan.mkv"}
	for _, name := range allFiles {
		vp := filepath.Join("movies", name)
		meta := metadataService.CreateFileMetadata(
			1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
			nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
		)
		require.NoError(t, metadataService.WriteFileMetadata(vp, meta))
	}

	// Create library directory with symlinks for all except "orphan.mkv"
	libraryDir := filepath.Join(tempDir, "library")
	require.NoError(t, os.MkdirAll(filepath.Join(libraryDir, "movies"), 0755))
	mountPath := "/mnt/test"
	for _, name := range allFiles[:5] {
		target := filepath.Join(mountPath, "movies", name)
		link := filepath.Join(libraryDir, "movies", name)
		require.NoError(t, os.Symlink(target, link))
	}

	healthEnabled := true
	cleanupEnabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &healthEnabled
	cfg.Health.LibrarySyncIntervalMinutes = 60
	cfg.Health.LibrarySyncConcurrency = 1
	cfg.Health.CleanupOrphanedMetadata = &cleanupEnabled
	cfg.Health.LibraryDir = &libraryDir
	cfg.Metadata.RootPath = tempDir
	cfg.MountPath = mountPath
	cfg.Import.ImportStrategy = config.ImportStrategySYMLINK

	configManager := config.NewManager(cfg, "")

	worker := NewLibrarySyncWorker(
		metadataService,
		healthRepo,
		configManager.GetConfig,
		configManager,
		&MockRcloneClient{},
	)

	ctx := context.Background()

	// First sync: orphan detected but NOT deleted (added to pending)
	worker.SyncLibrary(ctx, false)

	// Orphan metadata should still exist after first pass
	assert.True(t, metadataService.FileExists(filepath.Join("movies", "orphan.mkv")),
		"orphan should NOT be deleted after first sync (pending)")

	// Check pending state was saved
	raw, err := healthRepo.GetSystemState(ctx, "pending_metadata_deletions")
	require.NoError(t, err)
	assert.Contains(t, raw, "orphan.mkv", "orphan should be in pending state")

	// Second sync: confirmed orphan → deleted
	worker.SyncLibrary(ctx, false)

	assert.False(t, metadataService.FileExists(filepath.Join("movies", "orphan.mkv")),
		"orphan should be deleted after second sync (confirmed)")

	// Other files should still exist
	for _, name := range allFiles[:5] {
		assert.True(t, metadataService.FileExists(filepath.Join("movies", name)),
			"%s should NOT be deleted", name)
	}
}

func TestRatioGuard_SkipsCleanupWhenOrphanRatioHigh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}

	tempDir := t.TempDir()
	db := newResilienceDB(t)
	healthRepo := database.NewHealthRepository(db, database.DialectSQLite)
	metadataService := metadata.NewMetadataService(tempDir)

	// Create 4 metadata files, 2 have library entries, 2 are orphans (50% > 20%)
	for i := range 4 {
		vp := filepath.Join("movies", fmt.Sprintf("movie_%d.mkv", i))
		meta := metadataService.CreateFileMetadata(
			1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
			nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
		)
		require.NoError(t, metadataService.WriteFileMetadata(vp, meta))
	}

	// Library has only 2 of the 4 files
	libraryDir := filepath.Join(tempDir, "library")
	require.NoError(t, os.MkdirAll(filepath.Join(libraryDir, "movies"), 0755))
	mountPath := "/mnt/test"
	for i := range 2 {
		target := filepath.Join(mountPath, "movies", fmt.Sprintf("movie_%d.mkv", i))
		link := filepath.Join(libraryDir, "movies", fmt.Sprintf("movie_%d.mkv", i))
		require.NoError(t, os.Symlink(target, link))
	}

	healthEnabled := true
	cleanupEnabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &healthEnabled
	cfg.Health.LibrarySyncIntervalMinutes = 60
	cfg.Health.LibrarySyncConcurrency = 1
	cfg.Health.CleanupOrphanedMetadata = &cleanupEnabled
	cfg.Health.LibraryDir = &libraryDir
	cfg.Metadata.RootPath = tempDir
	cfg.MountPath = mountPath
	cfg.Import.ImportStrategy = config.ImportStrategySYMLINK

	configManager := config.NewManager(cfg, "")

	worker := NewLibrarySyncWorker(
		metadataService,
		healthRepo,
		configManager.GetConfig,
		configManager,
		&MockRcloneClient{},
	)

	ctx := context.Background()

	// First sync — would add to pending, but ratio guard should prevent it
	worker.SyncLibrary(ctx, false)

	// Second sync — even after two passes, ratio guard should still block cleanup
	worker.SyncLibrary(ctx, false)

	// All 4 metadata files should still exist (ratio guard prevented deletion)
	for i := range 4 {
		vp := filepath.Join("movies", fmt.Sprintf("movie_%d.mkv", i))
		assert.True(t, metadataService.FileExists(vp),
			"movie_%d.mkv should NOT be deleted (ratio guard)", i)
	}

	// Pending state should be cleared because ratio guard disabled cleanup
	raw, err := healthRepo.GetSystemState(ctx, "pending_metadata_deletions")
	assert.NoError(t, err)
	assert.Empty(t, raw, "pending state should be cleared when ratio guard triggers")
}

func TestWalkErrors_SkipCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}

	tempDir := t.TempDir()
	db := newResilienceDB(t)
	healthRepo := database.NewHealthRepository(db, database.DialectSQLite)
	metadataService := metadata.NewMetadataService(tempDir)

	// Create an orphan metadata file
	vp := filepath.Join("movies", "orphan.mkv")
	meta := metadataService.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	require.NoError(t, metadataService.WriteFileMetadata(vp, meta))

	// Create library dir with an unreadable subdirectory to cause walk errors
	libraryDir := filepath.Join(tempDir, "library")
	unreadableDir := filepath.Join(libraryDir, "movies", "unreadable")
	require.NoError(t, os.MkdirAll(unreadableDir, 0755))
	// Also create a valid symlink so library isn't empty (which triggers mount guard instead)
	mountPath := "/mnt/test"
	dummyLink := filepath.Join(libraryDir, "movies", "dummy.mkv")
	require.NoError(t, os.Symlink(filepath.Join(mountPath, "movies", "dummy.mkv"), dummyLink))

	// Make the subdirectory unreadable
	require.NoError(t, os.Chmod(unreadableDir, 0000))
	t.Cleanup(func() {
		// Restore permissions so t.TempDir() cleanup succeeds
		os.Chmod(unreadableDir, 0755)
	})

	// Also seed the pending state to simulate a previous pass marking this as orphaned
	pendingData := `{"movies/orphan.mkv":true}`
	require.NoError(t, healthRepo.UpdateSystemState(context.Background(), "pending_metadata_deletions", pendingData))

	healthEnabled := true
	cleanupEnabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &healthEnabled
	cfg.Health.LibrarySyncIntervalMinutes = 60
	cfg.Health.LibrarySyncConcurrency = 1
	cfg.Health.CleanupOrphanedMetadata = &cleanupEnabled
	cfg.Health.LibraryDir = &libraryDir
	cfg.Metadata.RootPath = tempDir
	cfg.MountPath = mountPath
	cfg.Import.ImportStrategy = config.ImportStrategySYMLINK

	configManager := config.NewManager(cfg, "")

	worker := NewLibrarySyncWorker(
		metadataService,
		healthRepo,
		configManager.GetConfig,
		configManager,
		&MockRcloneClient{},
	)

	ctx := context.Background()
	worker.SyncLibrary(ctx, false)

	// Orphan should NOT be deleted because walk errors disable cleanup
	assert.True(t, metadataService.FileExists(vp),
		"orphan should NOT be deleted when walk errors are present")
}
