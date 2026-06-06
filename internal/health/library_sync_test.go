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

// MockRcloneClient implements rclonecli.RcloneRcClient
type MockRcloneClient struct{}

func (m *MockRcloneClient) RefreshDir(ctx context.Context, provider string, dirs []string) error {
	return nil
}

func TestSyncLibrary_WorkerPool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	// Setup temporary directory for metadata
	tempDir, err := os.MkdirTemp("", "altmount_test_metadata")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Setup in-memory database
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	// Initialize database schema
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

	// Setup configuration
	healthEnabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &healthEnabled
	cfg.Health.LibrarySyncIntervalMinutes = 60
	cfg.Health.LibrarySyncConcurrency = 5 // Small concurrency for testing
	cfg.Metadata.RootPath = tempDir
	cfg.Import.ImportStrategy = config.ImportStrategyNone
	cfg.MountPath = "/mnt/test" // Dummy mount path

	configManager := config.NewManager(cfg, "")

	worker := NewLibrarySyncWorker(
		metadataService,
		healthRepo,
		configManager.GetConfig,
		configManager,
		&MockRcloneClient{},
	)

	// Create some metadata files
	numFiles := 50
	for i := range numFiles {
		fileName := filepath.Join("movies", "movie_"+fmt.Sprintf("%d", i)+".mkv")

		// Create a dummy metadata object
		meta := metadataService.CreateFileMetadata(
			100, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY, nil,
			metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
		)
		err := metadataService.WriteFileMetadata(fileName, meta)
		require.NoError(t, err)
	}

	// Run SyncLibrary
	ctx := context.Background()
	// dryRun = false
	result := worker.SyncLibrary(ctx, false)

	// SyncLibrary returns nil on success (non-dry-run)
	assert.Nil(t, result)

	// Check if files were added to database
	count, err := healthRepo.CountHealthItems(ctx, nil, nil, "")
	require.NoError(t, err)
	assert.Equal(t, numFiles, count)
}

func TestFindFilesToDelete_RepairTriggered(t *testing.T) {
	worker := &LibrarySyncWorker{}

	dbRecords := []database.AutomaticHealthCheckRecord{
		{
			FilePath: "movie.mkv",
			Status:   database.HealthStatusHealthy,
		},
		{
			FilePath: "repairing.mkv",
			Status:   database.HealthStatusRepairTriggered,
		},
		{
			FilePath: "deleted.mkv",
			Status:   database.HealthStatusHealthy,
		},
	}

	metaFileSet := map[string]string{
		"movie.mkv":     "path/to/meta/movie.mkv.meta",
		"repairing.mkv": "path/to/meta/repairing.mkv.meta",
		"deleted.mkv":   "path/to/meta/deleted.mkv.meta",
	}

	filesInLibrary := map[string]bool{
		"movie.mkv": true,
		// repairing.mkv is MISSING from filesInLibrary (simulating ARR deleted it)
		// deleted.mkv is MISSING from filesInLibrary
	}

	toDelete := worker.findFilesToDelete(context.Background(), dbRecords, metaFileSet, filesInLibrary)

	// repairing.mkv should be protected by its status
	// deleted.mkv should be marked for deletion
	require.Len(t, toDelete, 1)
	assert.Equal(t, "deleted.mkv", toDelete[0])
}

func TestMetaPathToMountRelativePath(t *testing.T) {
	sep := string(filepath.Separator)

	cases := []struct {
		name     string
		rootPath string
		metaPath string
		want     string
	}{
		{
			name:     "simple_root_no_trailing_sep",
			rootPath: "metadata",
			metaPath: filepath.Join("metadata", "complete", "foo", "bar.meta"),
			want:     "complete/foo/bar",
		},
		{
			name:     "root_with_trailing_sep",
			rootPath: "metadata" + sep,
			metaPath: filepath.Join("metadata", "complete", "foo", "bar.meta"),
			want:     "complete/foo/bar",
		},
		{
			name:     "root_with_dot_prefix",
			rootPath: "." + sep + "metadata",
			metaPath: filepath.Join("metadata", "complete", "foo", "bar.meta"),
			want:     "complete/foo/bar",
		},
		{
			name:     "absolute_root",
			rootPath: filepath.Join(string(filepath.Separator), "var", "lib", "metadata"),
			metaPath: filepath.Join(string(filepath.Separator), "var", "lib", "metadata", "complete", "foo", "bar.meta"),
			want:     "complete/foo/bar",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Metadata: config.MetadataConfig{RootPath: tc.rootPath}}
			worker := &LibrarySyncWorker{
				configGetter: func() *config.Config { return cfg },
			}
			got := worker.metaPathToMountRelativePath(tc.metaPath)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFindFilesToDelete_NormalizesBackslashes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("filepath.ToSlash only translates backslashes on Windows")
	}
	worker := &LibrarySyncWorker{}

	dbRecords := []database.AutomaticHealthCheckRecord{
		{FilePath: "complete\\foo\\bar.mkv", Status: database.HealthStatusHealthy},
		{FilePath: "complete/orphan.mkv", Status: database.HealthStatusHealthy},
	}
	metaFileSet := map[string]string{
		"complete/foo/bar.mkv": "metadata/complete/foo/bar.mkv.meta",
	}

	toDelete := worker.findFilesToDelete(context.Background(), dbRecords, metaFileSet, nil)
	require.Len(t, toDelete, 1)
	assert.Equal(t, "complete/orphan.mkv", toDelete[0])
}

func TestSyncLibrary_FiltersSampleFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	// Setup temporary directory for metadata
	tempDir, err := os.MkdirTemp("", "altmount_test_metadata")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Setup in-memory database
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	// Initialize database schema
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

	// Setup configuration
	healthEnabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &healthEnabled
	cfg.Health.LibrarySyncIntervalMinutes = 60
	cfg.Health.LibrarySyncConcurrency = 2
	cfg.Metadata.RootPath = tempDir
	cfg.Import.ImportStrategy = config.ImportStrategyNone
	cfg.MountPath = "/mnt/test"

	configManager := config.NewManager(cfg, "")

	worker := NewLibrarySyncWorker(
		metadataService,
		healthRepo,
		configManager.GetConfig,
		configManager,
		&MockRcloneClient{},
	)

	// Create a real file metadata (large)
	realFileName := filepath.Join("movies", "movie_real.mkv")
	realMeta := metadataService.CreateFileMetadata(
		500*1024*1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY, nil,
		metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	err = metadataService.WriteFileMetadata(realFileName, realMeta)
	require.NoError(t, err)

	// Create a sample file metadata (small, matches sample name)
	sampleFileName := filepath.Join("movies", "movie_real.sample.mkv")
	sampleMeta := metadataService.CreateFileMetadata(
		100*1024*1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY, nil,
		metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	err = metadataService.WriteFileMetadata(sampleFileName, sampleMeta)
	require.NoError(t, err)

	// Create a fake movie that contains "sample" in name but is large (800MB, should not be filtered)
	fakeMovieName := filepath.Join("movies", "The.Sample.Movie.2024.mkv")
	fakeMovieMeta := metadataService.CreateFileMetadata(
		800*1024*1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY, nil,
		metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	err = metadataService.WriteFileMetadata(fakeMovieName, fakeMovieMeta)
	require.NoError(t, err)

	// Create a 4K sample file (450MB, matches sample name and has 2160p, should be skipped since limit is 600MB for 4K)
	sample4KName := filepath.Join("movies", "movie_2160p.sample.mkv")
	sample4KMeta := metadataService.CreateFileMetadata(
		450*1024*1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY, nil,
		metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	err = metadataService.WriteFileMetadata(sample4KName, sample4KMeta)
	require.NoError(t, err)

	// Create a 4K real movie containing "sample" but is larger than 600MB (800MB, has 2160p, should not be skipped)
	real4KSampleName := filepath.Join("movies", "The.Sample.Movie.2160p.mkv")
	real4KSampleMeta := metadataService.CreateFileMetadata(
		800*1024*1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY, nil,
		metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	err = metadataService.WriteFileMetadata(real4KSampleName, real4KSampleMeta)
	require.NoError(t, err)

	// Run SyncLibrary
	ctx := context.Background()
	result := worker.SyncLibrary(ctx, false)
	assert.Nil(t, result)

	// Check that real and large movies are added, but sample movies are skipped (count = 3)
	count, err := healthRepo.CountHealthItems(ctx, nil, nil, "")
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Let's assert exactly which files are in the database
	items, err := healthRepo.ListHealthItems(ctx, nil, 10, 0, nil, "", "", "")
	require.NoError(t, err)
	
	foundReal := false
	foundFakeMovie := false
	foundSample := false
	foundSample4K := false
	foundReal4KSample := false

	for _, item := range items {
		if item.FilePath == "movies/movie_real.mkv" {
			foundReal = true
		} else if item.FilePath == "movies/The.Sample.Movie.2024.mkv" {
			foundFakeMovie = true
		} else if item.FilePath == "movies/movie_real.sample.mkv" {
			foundSample = true
		} else if item.FilePath == "movies/movie_2160p.sample.mkv" {
			foundSample4K = true
		} else if item.FilePath == "movies/The.Sample.Movie.2160p.mkv" {
			foundReal4KSample = true
		}
	}

	assert.True(t, foundReal, "Should find movie_real.mkv")
	assert.True(t, foundFakeMovie, "Should find The.Sample.Movie.2024.mkv")
	assert.True(t, foundReal4KSample, "Should find The.Sample.Movie.2160p.mkv")
	assert.False(t, foundSample, "Should NOT find movie_real.sample.mkv")
	assert.False(t, foundSample4K, "Should NOT find movie_2160p.sample.mkv")
}
