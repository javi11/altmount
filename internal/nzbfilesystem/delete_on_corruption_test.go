package nzbfilesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateFileHealthOnError_DeleteOnCorruption_DeletesFile verifies that when
// health.corruption_action is "delete", a mid-stream DataCorruptionError deletes the
// file's metadata, health record, and physical library file (plus any now-empty parent
// directory) instead of triggering an Arr repair.
func TestUpdateFileHealthOnError_DeleteOnCorruption_DeletesFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	repo, db, ms := setupStreamHealthEnv(t)
	ctx := context.Background()

	libraryRoot := t.TempDir()
	filePath := "series/stream.s01e03.mkv"
	libraryDir := filepath.Join(libraryRoot, "series", "stream")
	libraryPath := filepath.Join(libraryDir, "stream.s01e03.mkv")
	seg := writeStreamMeta(t, ms, filePath)

	require.NoError(t, os.MkdirAll(libraryDir, 0o755))
	require.NoError(t, os.WriteFile(libraryPath, []byte("dummy"), 0o644))

	_, err := db.Exec(
		`INSERT INTO file_health (file_path, library_path, status, scheduled_check_at) VALUES (?, ?, 'healthy', datetime('now'))`,
		filePath, libraryPath,
	)
	require.NoError(t, err)

	enabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &enabled
	cfg.Health.CorruptionAction = "delete"
	cfg.Health.LibraryDir = &libraryRoot
	cfg.MountPath = ""

	mvf := newStreamFailureMVF(ctx, filePath, repo, ms, seg, cfg)
	mvf.updateFileHealthOnError(&usenet.DataCorruptionError{UnderlyingErr: errors.New("article not found"), NoRetry: true}, true)

	fh, err := repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	assert.Nil(t, fh, "health record should be deleted")

	deletedMeta, err := ms.ReadFileMetadata(filePath)
	require.NoError(t, err)
	assert.Nil(t, deletedMeta, "metadata should be deleted")

	_, statErr := os.Stat(libraryPath)
	assert.True(t, os.IsNotExist(statErr), "physical library file should be deleted")

	_, dirErr := os.Stat(libraryDir)
	assert.True(t, os.IsNotExist(dirErr), "now-empty library directory should be removed")
}
