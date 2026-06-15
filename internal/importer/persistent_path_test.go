package importer

import (
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/nzbfile"
	"github.com/stretchr/testify/assert"
)

// TestPersistentNzbPath verifies that a persisted NZB lands directly in its category
// folder with a clean filename when the destination is free, and is namespaced with the
// queue item ID as a filename prefix when the plain path is already taken.
func TestPersistentNzbPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Movies")
	base := "Abbott Elementary (2021)"

	// Free destination: no number, file sits directly in the category folder.
	free := func(string) bool { return false }
	p1 := persistentNzbPath(dir, base, nzbfile.GzExtension, 42, free)
	assert.Equal(t, filepath.Join(dir, base+nzbfile.GzExtension), p1)

	// Plain extension when compression is disabled.
	plain := persistentNzbPath(dir, base, ".nzb", 42, free)
	assert.Equal(t, filepath.Join(dir, base+".nzb"), plain)

	// Collision: namespace with the queue item ID as a filename prefix, staying in nzbDir.
	taken := func(string) bool { return true }
	p2 := persistentNzbPath(dir, base, nzbfile.GzExtension, 202, taken)
	assert.Equal(t, filepath.Join(dir, "202-"+base+nzbfile.GzExtension), p2)
	assert.NotEqual(t, p1, p2)
}
