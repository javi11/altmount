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


func TestDeleteFileMetadataWithSourceNzb_RemovesMetadata(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := filepath.Join("movies", "test_movie.mkv")

	meta := ms.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "abcde12345",
	)
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	metaPath := ms.GetMetadataFilePath(virtualPath)
	require.FileExists(t, metaPath)

	ctx := context.Background()
	require.NoError(t, ms.DeleteFileMetadataWithSourceNzb(ctx, virtualPath, false))

	assert.NoFileExists(t, metaPath)
}

func TestDeleteFileMetadataWithSourceNzb_NoIDSidecar_NoError(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := filepath.Join("movies", "no_id_movie.mkv")

	meta := ms.CreateFileMetadata(
		512, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	ctx := context.Background()
	err := ms.DeleteFileMetadataWithSourceNzb(ctx, virtualPath, false)
	assert.NoError(t, err, "delete should succeed even without .id sidecar")

	assert.NoFileExists(t, ms.GetMetadataFilePath(virtualPath))
}

func TestMoveToCorrupted_MovesMetadata(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := filepath.Join("movies", "corrupted_movie.mkv")

	meta := ms.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "fghij67890",
	)
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	ctx := context.Background()
	require.NoError(t, ms.MoveToCorrupted(ctx, virtualPath))

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

	// Create a valid metadata file and manually plant a valid .ids/ symlink for it
	validPath := filepath.Join("movies", "valid.mkv")
	validID := "valid12345"
	meta := ms.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, validID,
	)
	require.NoError(t, ms.WriteFileMetadata(validPath, meta))

	// Manually create a valid .ids/ symlink pointing at the .meta file
	validMetaPath := ms.GetMetadataFilePath(validPath)
	validShardDir := filepath.Join(root, ".ids", "v", "a", "l", "i", "d")
	require.NoError(t, os.MkdirAll(validShardDir, 0755))
	validLink := filepath.Join(validShardDir, validID+".meta")
	require.NoError(t, os.Symlink(validMetaPath, validLink))

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
