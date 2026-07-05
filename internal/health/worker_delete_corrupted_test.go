package health

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/holes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_DeleteOnCorruption_HealthCheckExhausted verifies that when
// health.corruption_action is "delete", a health-check cycle that exhausts retries
// deletes the file's metadata, health record, and physical library file (plus any
// now-empty parent directory) instead of triggering an Arr repair.
func TestE2E_DeleteOnCorruption_HealthCheckExhausted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	tempDir := t.TempDir()
	libraryRoot := t.TempDir()
	env := newRepairTestEnv(t, tempDir, nil, func(c *config.Config) {
		c.Health.CorruptionAction = "delete"
		c.Health.LibraryDir = &libraryRoot
	})

	ctx := context.Background()
	filePath := "series/show.s01e01.mkv"
	libraryDir := filepath.Join(libraryRoot, "series", "show")
	libraryPath := filepath.Join(libraryDir, "show.s01e01.mkv")
	maxRetries := 3

	require.NoError(t, os.MkdirAll(libraryDir, 0o755))
	require.NoError(t, os.WriteFile(libraryPath, []byte("dummy"), 0o644))

	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))

	// File already at its last health retry: a single failing cycle would normally
	// trigger a repair, but corruption_action is "delete".
	insertFileHealth(t, env.db, filePath, libraryPath, maxRetries-1, maxRetries)

	require.NoError(t, env.hw.runHealthCheckCycle(ctx))

	env.mockARRs.mu.Lock()
	callCount := len(env.mockARRs.calls)
	env.mockARRs.mu.Unlock()
	assert.Equal(t, 0, callCount, "delete mode must not trigger an ARR rescan")

	fh, err := env.healthRepo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	assert.Nil(t, fh, "health record should be deleted")

	deletedMeta, err := env.metadataService.ReadFileMetadata(filePath)
	require.NoError(t, err)
	assert.Nil(t, deletedMeta, "metadata should be deleted")

	_, statErr := os.Stat(libraryPath)
	assert.True(t, os.IsNotExist(statErr), "physical library file should be deleted")

	_, dirErr := os.Stat(libraryDir)
	assert.True(t, os.IsNotExist(dirErr), "now-empty library directory should be removed")

	_, rootErr := os.Stat(libraryRoot)
	assert.NoError(t, rootErr, "library root itself must not be removed")
}

// TestE2E_DeleteOnCorruption_AlreadyRepairTriggered verifies that a file already sitting
// in repair_triggered (e.g. from before corruption_action was switched to "delete") gets
// deleted on the next repair-retry sweep instead of being re-notified to the ARR.
func TestE2E_DeleteOnCorruption_AlreadyRepairTriggered(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	tempDir := t.TempDir()
	env := newRepairTestEnv(t, tempDir, nil, func(c *config.Config) {
		c.Health.CorruptionAction = "delete"
	})

	ctx := context.Background()
	filePath := "series/show.s01e02.mkv"

	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))

	_, err := env.db.Exec(`
		INSERT INTO file_health
			(file_path, library_path, status, retry_count, max_retries,
			 repair_retry_count, max_repair_retries, scheduled_check_at)
		VALUES (?, NULL, 'repair_triggered', 3, 3, 0, 3, datetime('now', '-1 second'))
	`, filePath)
	require.NoError(t, err)

	require.NoError(t, env.hw.runHealthCheckCycle(ctx))

	env.mockARRs.mu.Lock()
	callCount := len(env.mockARRs.calls)
	env.mockARRs.mu.Unlock()
	assert.Equal(t, 0, callCount, "delete mode must not re-notify the ARR")

	fh, err := env.healthRepo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	assert.Nil(t, fh, "health record should be deleted")
}

// TestPrepareUpdateForResultDeleteOnCorruption verifies that a degraded verdict still wins
// over delete-on-corruption: a file that is playable despite missing segments must never
// be deleted just because corruption_action is set to "delete".
func TestPrepareUpdateForResultDeleteOnCorruption(t *testing.T) {
	tempDir := t.TempDir()
	env := newRepairTestEnv(t, tempDir, nil, func(c *config.Config) {
		c.Health.CorruptionAction = "delete"
	})

	filePath := "/movies/movie.mp4"
	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))

	fh := database.FileHealth{
		FilePath:  filePath,
		Status:    database.HealthStatusPending,
		RetryCount: 99,
	}
	event := HealthEvent{
		Type:     EventTypeFileCorrupted,
		FilePath: filePath,
		Status:   database.HealthStatusCorrupted,
		Classification: &holes.Impact{
			Verdict:      holes.VerdictDegraded,
			TotalMissing: 2,
			LongestRun:   1,
		},
	}

	update, sideEffect := env.hw.prepareUpdateForResult(context.Background(), &fh, event)
	assert.Equal(t, database.UpdateTypeDegraded, update.Type, "degraded must win over delete-on-corruption")
	require.NoError(t, sideEffect())

	got, err := env.metadataService.ReadFileMetadata(filePath)
	require.NoError(t, err)
	assert.NotNil(t, got, "degraded file must not be deleted")
}
