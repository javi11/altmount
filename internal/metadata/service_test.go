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

// TestReadFileMetadataLite_DoesNotReadFullProto pins the fast path: when the
// `.meta` proto is multi-MB (because the file has thousands of NestedSources
// or SegmentData entries — the exact shape that caused a 7.94 GB
// PROPFIND allocation spike), ReadFileMetadataLite must read only the head
// of the file and never instantiate the giant proto. We measure this via
// the file size we write vs. the bytes read by the lite path.
func TestReadFileMetadataLite_DoesNotReadFullProto(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := filepath.Join("movies", "huge.m2ts")

	// Build a FileMetadata with thousands of NestedSources so the on-disk
	// proto is hundreds of KB — large enough that a regression to the
	// full os.ReadFile + proto.Unmarshal path would allocate >>liteScanBytes
	// and be caught by the heap-delta assertion below.
	nested := make([]*metapb.NestedSegmentSource, 0, 5000)
	for i := range 5000 {
		nested = append(nested, &metapb.NestedSegmentSource{
			Segments: []*metapb.SegmentData{
				{Id: "msg-id-with-a-typical-length@server.example", StartOffset: int64(i * 1024), EndOffset: int64((i + 1) * 1024), SegmentSize: 1024},
			},
			InnerOffset:     0,
			InnerLength:     1024,
			InnerVolumeSize: 1024,
		})
	}
	meta := ms.CreateFileMetadata(
		17_860_995_072, "Avatar.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "huge-nzbdav-id",
	)
	meta.NestedSources = nested
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	// Confirm the on-disk file is at least 200 KB — the partial-read
	// budget is 4 KB so anything substantially larger gives the heap-delta
	// assertion enough headroom to catch a regression.
	stat, err := os.Stat(ms.GetMetadataFilePath(virtualPath))
	require.NoError(t, err)
	require.Greater(t, stat.Size(), int64(200<<10), "test setup should produce a >200KB .meta file to make the fast-path savings observable")

	// Drop the liteCache entry written by WriteFileMetadata so we hit the
	// disk-read path under test.
	ms.liteCache.Purge()

	// Snapshot heap allocations before / after the call. The full-read
	// implementation would allocate at least stat.Size() bytes (for the
	// os.ReadFile buffer) plus the unmarshalled proto. The partial-read
	// implementation should allocate well under 64 KiB.
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	lite, err := ms.ReadFileMetadataLite(virtualPath)
	require.NoError(t, err)
	require.NotNil(t, lite)

	runtime.ReadMemStats(&after)
	delta := after.TotalAlloc - before.TotalAlloc
	t.Logf("ReadFileMetadataLite allocated %d bytes (on-disk .meta = %d bytes)", delta, stat.Size())

	// Correctness: lite must reflect the values we wrote.
	assert.Equal(t, int64(17_860_995_072), lite.FileSize)
	assert.Equal(t, metapb.FileStatus_FILE_STATUS_HEALTHY, lite.Status)

	// Regression guard: the fast path must allocate dramatically less than
	// the full file. Use 5× liteScanBytes as a comfortable upper bound that
	// still catches a regression where the implementation re-reads the
	// whole file.
	const maxExpectedAlloc = 5 * liteScanBytes
	assert.LessOrEqualf(t, delta, uint64(maxExpectedAlloc),
		"ReadFileMetadataLite allocated %d bytes — should be ≤ %d. A regression to the full os.ReadFile + proto.Unmarshal would allocate >= the on-disk size (%d).",
		delta, maxExpectedAlloc, stat.Size())
}

// TestReadFileMetadataLite_FallsBackOnLongHeader covers the edge where the
// lite fields aren't reachable within liteScanBytes (e.g., a future schema
// change places one after a very large field). The fallback path produces
// the same correct lite struct, just by reading the full file.
func TestReadFileMetadataLite_FallsBackOnLongHeader(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := filepath.Join("movies", "long-header.mkv")

	// Craft a SourceNzbPath longer than liteScanBytes so the lite fields
	// after it (status, modified_at) fall past the partial-read window.
	// file_size (field 1) is before it, so the partial-read scan sees
	// FileSize but not Status/ModifiedAt → falls back to full read.
	longPath := make([]byte, liteScanBytes+512)
	for i := range longPath {
		longPath[i] = 'a'
	}
	meta := ms.CreateFileMetadata(
		1234, string(longPath), metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "fallback-id",
	)
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))
	ms.liteCache.Purge()

	lite, err := ms.ReadFileMetadataLite(virtualPath)
	require.NoError(t, err)
	require.NotNil(t, lite)
	assert.Equal(t, int64(1234), lite.FileSize)
	assert.Equal(t, metapb.FileStatus_FILE_STATUS_HEALTHY, lite.Status)
}
