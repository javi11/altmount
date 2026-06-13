package importer

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUniqueFailedNzbPath verifies that failed NZBs sharing a basename resolve to distinct
// destination paths, so MoveToFailedFolder's UNIQUE import_queue.nzb_path update cannot collide.
func TestUniqueFailedNzbPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join("incoming", "Abbott Elementary (2021).nzb.gz")

	// First failed item: plain basename, since nothing occupies it yet.
	p1 := uniqueFailedNzbPath(dir, src, 101)
	assert.Equal(t, filepath.Join(dir, "Abbott Elementary (2021).nzb.gz"), p1)

	// Simulate the move MoveToFailedFolder performs.
	require.NoError(t, os.WriteFile(p1, []byte("nzb"), 0o644))

	// Second failed item with the SAME basename must not reuse the taken path.
	p2 := uniqueFailedNzbPath(dir, src, 202)
	assert.NotEqual(t, p1, p2, "colliding basename must get a distinct path")
	assert.Equal(t, filepath.Join(dir, "202-Abbott Elementary (2021).nzb.gz"), p2)

	// A third collision is namespaced by its own ID and stays unique.
	require.NoError(t, os.WriteFile(p2, []byte("nzb"), 0o644))
	p3 := uniqueFailedNzbPath(dir, src, 303)
	assert.Equal(t, filepath.Join(dir, "303-Abbott Elementary (2021).nzb.gz"), p3)
	assert.NotEqual(t, p1, p3)
	assert.NotEqual(t, p2, p3)
}

// newFailedFolderTestService builds the minimal Service needed to exercise
// MoveToFailedFolder: a configGetter so GetFailedNzbFolder resolves under a temp
// dir, and a logger. database is intentionally left nil — the retry no-op path
// must return before ever touching the repository, so a nil deref here would be a
// real regression, not a test artifact.
func newFailedFolderTestService(t *testing.T) (*Service, string) {
	t.Helper()
	configDir := t.TempDir()
	s := &Service{
		log: slog.Default(),
		configGetter: func() *config.Config {
			return &config.Config{
				Database: config.DatabaseConfig{Path: filepath.Join(configDir, "altmount.db")},
			}
		},
	}
	return s, s.GetFailedNzbFolder()
}

// TestMoveToFailedFolder_RetryOfFailedItemIsNoop covers the concern that
// namespacing the failed path breaks retry. After an item fails, its NzbPath
// already points inside the failed folder. Clicking "retry" reprocesses it; if it
// fails again MoveToFailedFolder runs with the item ALREADY in failedDir. Without
// the early "already in failed folder" guard, uniqueFailedNzbPath would see the
// item's own file at the plain path and rename it to a "<id>-" duplicate, drifting
// the on-disk NZB from the DB row. This asserts it stays a no-op.
func TestMoveToFailedFolder_RetryOfFailedItemIsNoop(t *testing.T) {
	s, failedDir := newFailedFolderTestService(t)
	require.NoError(t, os.MkdirAll(failedDir, 0o755))

	failedNzb := filepath.Join(failedDir, "Retry Me (2026).nzb")
	require.NoError(t, os.WriteFile(failedNzb, []byte("nzb"), 0o644))

	item := &database.ImportQueueItem{ID: 7, NzbPath: failedNzb}
	require.NoError(t, s.MoveToFailedFolder(context.Background(), item))

	assert.Equal(t, failedNzb, item.NzbPath, "retry must not rewrite the failed item's path")
	assert.FileExists(t, failedNzb)
	assert.NoFileExists(t, filepath.Join(failedDir, "7-Retry Me (2026).nzb"),
		"the failed item's own file must not be namespaced into a duplicate on retry")
}

// TestMoveToFailedFolder_RetryOfFailedItemInCategoryIsNoop is the same guarantee
// for a categorized item, whose failed copy lives in failedDir/<category>. The
// early-return must compare against the category-qualified failedDir.
func TestMoveToFailedFolder_RetryOfFailedItemInCategoryIsNoop(t *testing.T) {
	s, failedRoot := newFailedFolderTestService(t)
	category := "tv"
	failedDir := filepath.Join(failedRoot, category)
	require.NoError(t, os.MkdirAll(failedDir, 0o755))

	failedNzb := filepath.Join(failedDir, "Show S01E01.nzb")
	require.NoError(t, os.WriteFile(failedNzb, []byte("nzb"), 0o644))

	item := &database.ImportQueueItem{ID: 9, NzbPath: failedNzb, Category: &category}
	require.NoError(t, s.MoveToFailedFolder(context.Background(), item))

	assert.Equal(t, failedNzb, item.NzbPath)
	assert.FileExists(t, failedNzb)
	assert.NoFileExists(t, filepath.Join(failedDir, "9-Show S01E01.nzb"))
}
