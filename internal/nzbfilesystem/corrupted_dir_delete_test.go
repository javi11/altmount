package nzbfilesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCorruptedDirFixture(t *testing.T) (*MetadataRemoteFile, string, string) {
	t.Helper()

	root := t.TempDir()
	ms := metadata.NewMetadataService(root)

	filePath := "series/stream.s01e01.mkv"
	writeStreamMeta(t, ms, filePath)
	require.NoError(t, ms.MoveToCorrupted(context.Background(), filePath))

	copyPath := filepath.Join(root, "corrupted_metadata", "series", "stream.s01e01.mkv.meta")
	require.FileExists(t, copyPath)

	cfg := config.DefaultConfig()
	mrf := &MetadataRemoteFile{
		metadataService: ms,
		configGetter:    func() *config.Config { return cfg },
	}

	return mrf, root, copyPath
}

func TestRemoveFile_CorruptedFolderIsProtected(t *testing.T) {
	mrf, root, copyPath := newCorruptedDirFixture(t)

	for _, name := range []string{"corrupted_metadata", "/corrupted_metadata", "/corrupted_metadata/"} {
		ok, err := mrf.RemoveFile(context.Background(), name)
		assert.False(t, ok, name)
		require.ErrorIs(t, err, os.ErrPermission, name)
	}

	assert.DirExists(t, filepath.Join(root, "corrupted_metadata"))
	assert.FileExists(t, copyPath, "a recursive DELETE must not wipe the safety copies")
}

func TestRemoveFile_FileInsideCorruptedFolderIsRemovable(t *testing.T) {
	mrf, _, copyPath := newCorruptedDirFixture(t)

	ok, err := mrf.RemoveFile(context.Background(), "corrupted_metadata/series/stream.s01e01.mkv")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.NoFileExists(t, copyPath)
}
