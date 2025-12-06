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
