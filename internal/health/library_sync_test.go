package health

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			release_date DATETIME,
			scheduled_check_at DATETIME
		);
	`)
	require.NoError(t, err)

	healthRepo := database.NewHealthRepository(db)
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
	for i := 0; i < numFiles; i++ {
		fileName := filepath.Join("movies", "movie_"+fmt.Sprintf("%d", i)+".mkv")
		
		// Create a dummy metadata object
		meta := metadataService.CreateFileMetadata(
			1000, 
			"source.nzb", 
			0, // Status
			nil, 
			0, // Encryption
			"", "", 
			time.Now().Unix(), 
			nil,
			"",
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

	filesInUse := map[string]string{
		"movie.mkv": "path/to/lib/link",
		// repairing.mkv is MISSING from filesInUse (simulating ARR deleted it)
		// deleted.mkv is MISSING from filesInUse
	}

	toDelete := worker.findFilesToDelete(context.Background(), dbRecords, metaFileSet, filesInUse)

	// repairing.mkv should be protected by its status
	// deleted.mkv should be marked for deletion
	require.Len(t, toDelete, 1)
	assert.Equal(t, "deleted.mkv", toDelete[0])
}
