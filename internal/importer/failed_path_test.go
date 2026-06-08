package importer

import (
	"os"
	"path/filepath"
	"testing"

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
