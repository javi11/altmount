package metadata

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMetaWithIDSymlink creates a MetadataService at t.TempDir(), writes metadata +
// .id sidecar + .ids/ symlink via UpdateIDSymlink. Returns the service and root path.
func setupMetaWithIDSymlink(t *testing.T, virtualPath, nzbdavID string) (*MetadataService, string) {
	t.Helper()

	root := t.TempDir()
	ms := NewMetadataService(root)

	meta := ms.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, nzbdavID,
	)
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	// WriteFileMetadata writes the .id sidecar when nzbdavID != "".
	// Now create the .ids/ symlink.
	require.NoError(t, ms.UpdateIDSymlink(nzbdavID, virtualPath))

	// Verify symlink was created
	metaPath := ms.GetMetadataFilePath(virtualPath)
	require.FileExists(t, metaPath)
	require.FileExists(t, metaPath+".id")

	return ms, root
}

// idSymlinkPath returns the expected .ids/ symlink path for a given nzbdavID.
func idSymlinkPath(root, nzbdavID string) string {
	id := nzbdavID
	return filepath.Join(root, ".ids", string(id[0]), string(id[1]), string(id[2]), string(id[3]), string(id[4]), id+".meta")
}

func TestDeleteFileMetadataWithSourceNzb_RemovesIDSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}

	virtualPath := filepath.Join("movies", "test_movie.mkv")
	nzbdavID := "abcde12345"

	ms, root := setupMetaWithIDSymlink(t, virtualPath, nzbdavID)

	// Verify the symlink exists before deletion
	linkPath := idSymlinkPath(root, nzbdavID)
	_, err := os.Lstat(linkPath)
	require.NoError(t, err, "symlink should exist before delete")

	// Delete metadata
	ctx := context.Background()
	require.NoError(t, ms.DeleteFileMetadataWithSourceNzb(ctx, virtualPath, false))

	// Verify .ids/ symlink was removed
	_, err = os.Lstat(linkPath)
	assert.True(t, os.IsNotExist(err), "symlink should be removed after delete")

	// Verify .meta and .id files are also gone
	metaPath := ms.GetMetadataFilePath(virtualPath)
	assert.NoFileExists(t, metaPath)
	assert.NoFileExists(t, metaPath+".id")
}

func TestDeleteFileMetadataWithSourceNzb_NoIDSidecar_NoError(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := filepath.Join("movies", "no_id_movie.mkv")

	// Write metadata without nzbdavID (no .id sidecar)
	meta := ms.CreateFileMetadata(
		512, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	ctx := context.Background()
	err := ms.DeleteFileMetadataWithSourceNzb(ctx, virtualPath, false)
	assert.NoError(t, err, "delete should succeed even without .id sidecar")

	// Meta file gone
	assert.NoFileExists(t, ms.GetMetadataFilePath(virtualPath))
}

func TestMoveToCorrupted_RemovesIDSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}

	virtualPath := filepath.Join("movies", "corrupted_movie.mkv")
	nzbdavID := "fghij67890"

	ms, root := setupMetaWithIDSymlink(t, virtualPath, nzbdavID)

	linkPath := idSymlinkPath(root, nzbdavID)
	_, err := os.Lstat(linkPath)
	require.NoError(t, err, "symlink should exist before move")

	ctx := context.Background()
	require.NoError(t, ms.MoveToCorrupted(ctx, virtualPath))

	// Symlink removed
	_, err = os.Lstat(linkPath)
	assert.True(t, os.IsNotExist(err), "symlink should be removed after move to corrupted")

	// Original location gone
	assert.NoFileExists(t, ms.GetMetadataFilePath(virtualPath))

	// Metadata now in corrupted folder
	corruptedPath := filepath.Join(root, "corrupted_metadata", "movies", "corrupted_movie.mkv.meta")
	assert.FileExists(t, corruptedPath, "metadata should exist in corrupted folder")
}

func TestCleanupOrphanedIDSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}

	root := t.TempDir()
	ms := NewMetadataService(root)

	// Create a valid metadata + symlink
	validPath := filepath.Join("movies", "valid.mkv")
	validID := "valid12345"
	meta := ms.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, validID,
	)
	require.NoError(t, ms.WriteFileMetadata(validPath, meta))
	require.NoError(t, ms.UpdateIDSymlink(validID, validPath))

	// Create a broken symlink (target does not exist)
	brokenID := "broke12345"
	brokenShardDir := filepath.Join(root, ".ids", "b", "r", "o", "k", "e")
	require.NoError(t, os.MkdirAll(brokenShardDir, 0755))
	brokenLink := filepath.Join(brokenShardDir, brokenID+".meta")
	require.NoError(t, os.Symlink("/nonexistent/target.meta", brokenLink))

	ctx := context.Background()
	removed, err := ms.CleanupOrphanedIDSymlinks(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, removed, "should remove exactly one orphaned symlink")

	// Broken symlink gone
	_, err = os.Lstat(brokenLink)
	assert.True(t, os.IsNotExist(err), "broken symlink should be removed")

	// Valid symlink still present
	validLink := idSymlinkPath(root, validID)
	_, err = os.Lstat(validLink)
	assert.NoError(t, err, "valid symlink should still exist")
}

func TestCleanupOrphanedIDSymlinks_NoIDsDir(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	removed, err := ms.CleanupOrphanedIDSymlinks(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 0, removed)
}

func TestCleanupOrphanedIDSymlinks_ContextCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}

	root := t.TempDir()
	ms := NewMetadataService(root)

	// Create a few broken symlinks
	for _, id := range []string{"aaaaa11111", "bbbbb22222", "ccccc33333"} {
		shardDir := filepath.Join(root, ".ids", string(id[0]), string(id[1]), string(id[2]), string(id[3]), string(id[4]))
		require.NoError(t, os.MkdirAll(shardDir, 0755))
		require.NoError(t, os.Symlink("/nonexistent/"+id+".meta", filepath.Join(shardDir, id+".meta")))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := ms.CleanupOrphanedIDSymlinks(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}
