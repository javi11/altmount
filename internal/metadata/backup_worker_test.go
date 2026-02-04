package metadata

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestBackupWorker_performBackup(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "metadata-test-*")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	metadataDir := filepath.Join(tempDir, "metadata")
	backupRoot := filepath.Join(tempDir, "backups")
	err = os.MkdirAll(metadataDir, 0755)
	assert.NoError(t, err)

	// Create some dummy .meta files in subdirectories
	err = os.MkdirAll(filepath.Join(metadataDir, "movies"), 0755)
	assert.NoError(t, err)

	metaFiles := []string{
		filepath.Join("movies", "test1.meta"),
		"test2.meta",
		"test3.txt",
	}
	for _, f := range metaFiles {
		err = os.WriteFile(filepath.Join(metadataDir, f), []byte("content"), 0644)
		assert.NoError(t, err)
	}

	enabled := true
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			RootPath: metadataDir,
			Backup: config.MetadataBackupConfig{
				Enabled:       &enabled,
				IntervalHours: 24,
				KeepBackups:   2,
				Path:          backupRoot,
			},
		},
	}

	configGetter := func() *config.Config {
		return cfg
	}

	worker := NewBackupWorker(configGetter)

	// Run backup
	worker.performBackup()

	// Check if backup directory exists
	dirs, err := os.ReadDir(backupRoot)
	assert.NoError(t, err)
	assert.Len(t, dirs, 1)
	assert.True(t, dirs[0].IsDir())

	backupDir := filepath.Join(backupRoot, dirs[0].Name())
	
	// Verify copied files and structure
	assert.FileExists(t, filepath.Join(backupDir, "movies", "test1.meta"))
	assert.FileExists(t, filepath.Join(backupDir, "test2.meta"))
	assert.NoFileExists(t, filepath.Join(backupDir, "test3.txt"))

	// Test cleanup: create more backups
	time.Sleep(1 * time.Second)
	worker.performBackup()
	time.Sleep(1 * time.Second)
	worker.performBackup()

	dirs, err = os.ReadDir(backupRoot)
	assert.NoError(t, err)
	assert.Len(t, dirs, 2) // Should keep only 2 latest folders
}
