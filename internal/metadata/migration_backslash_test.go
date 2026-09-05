package metadata

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plantLegacyBackslashMeta writes a .meta the way the pre-normalization code did:
// straight at the un-normalized name, backslash and all.
func plantLegacyBackslashMeta(t *testing.T, ms *MetadataService, relWithBackslash string, size int64) string {
	t.Helper()

	legacyPath := filepath.Join(ms.rootPath, relWithBackslash+".meta")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0755))

	meta := ms.CreateFileMetadata(
		size, "legacy.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "legacy12345",
	)
	// Write through the service to a scratch path, then move it into the legacy
	// location, so the on-disk bytes are a genuine .meta.
	require.NoError(t, ms.WriteFileMetadata("scratch/tmp.mkv", meta))
	require.NoError(t, os.Rename(ms.GetMetadataFilePath("scratch/tmp.mkv"), legacyPath))
	require.NoError(t, os.WriteFile(legacyPath+".id", []byte("legacy12345"), 0644))

	return legacyPath
}

func TestMigrateBackslashPaths_RelocatesStrandedMetadata(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	legacyPath := plantLegacyBackslashMeta(t, ms, `movies/Release\file.mkv`, 2048)
	require.FileExists(t, legacyPath)

	// The virtual path the rest of the system now resolves to.
	normalized := "movies/Release/file.mkv"
	require.NoFileExists(t, ms.GetMetadataFilePath(normalized),
		"test premise: nothing at the normalized location yet")

	res, err := ms.MigrateBackslashPaths(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Moved)
	assert.Equal(t, 0, res.Skipped)

	assert.NoFileExists(t, legacyPath, "the stranded file should be gone")
	require.FileExists(t, ms.GetMetadataFilePath(normalized))
	assert.FileExists(t, ms.GetMetadataFilePath(normalized)+".id", "the .id sidecar rides along")

	// The whole point: it is readable again through the normal accessor.
	got, err := ms.ReadFileMetadata(normalized)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(2048), got.FileSize)
}

func TestMigrateBackslashPaths_IsIdempotent(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	plantLegacyBackslashMeta(t, ms, `movies/Release\file.mkv`, 1024)

	first, err := ms.MigrateBackslashPaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, first.Moved)

	second, err := ms.MigrateBackslashPaths(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, second.Moved, "a second run has nothing left to do")
	assert.Equal(t, 0, second.Skipped)
}

func TestMigrateBackslashPaths_DoesNotClobberExistingFile(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	// A legitimate file already owns the normalized name.
	normalized := "movies/Release/file.mkv"
	live := ms.CreateFileMetadata(
		9999, "live.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "livefile001",
	)
	require.NoError(t, ms.WriteFileMetadata(normalized, live))

	legacyPath := plantLegacyBackslashMeta(t, ms, `movies/Release\file.mkv`, 1111)

	res, err := ms.MigrateBackslashPaths(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, res.Moved)
	assert.Equal(t, 1, res.Skipped)

	assert.FileExists(t, legacyPath, "the stranded file stays put rather than being lost")

	got, err := ms.ReadFileMetadata(normalized)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(9999), got.FileSize, "the live file must not be overwritten")
}

func TestMigrateBackslashPaths_BackslashInDirectoryComponent(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	plantLegacyBackslashMeta(t, ms, `movies\Release\file.mkv`, 512)

	res, err := ms.MigrateBackslashPaths(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Moved)

	got, err := ms.ReadFileMetadata("movies/Release/file.mkv")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(512), got.FileSize)
}

func TestMigrateBackslashPaths_NoopOnCleanTree(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	meta := ms.CreateFileMetadata(
		100, "clean.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "cleanfile01",
	)
	require.NoError(t, ms.WriteFileMetadata("movies/clean.mkv", meta))

	res, err := ms.MigrateBackslashPaths(context.Background())
	require.NoError(t, err)
	assert.Equal(t, BackslashSweepResult{}, res)
	assert.FileExists(t, ms.GetMetadataFilePath("movies/clean.mkv"))
}

func TestMigrateBackslashPaths_ContextCancellation(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	plantLegacyBackslashMeta(t, ms, `movies/Release\file.mkv`, 256)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ms.MigrateBackslashPaths(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}
