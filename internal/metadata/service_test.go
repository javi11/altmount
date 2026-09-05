package metadata

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/holes"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteFileMetadata_RemovesMetadata(t *testing.T) {
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
	require.NoError(t, ms.DeleteFileMetadata(ctx, virtualPath))

	assert.NoFileExists(t, metaPath)
}

func TestDeleteFileMetadata_NoIDSidecar_NoError(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := filepath.Join("movies", "no_id_movie.mkv")

	meta := ms.CreateFileMetadata(
		512, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	ctx := context.Background()
	err := ms.DeleteFileMetadata(ctx, virtualPath)
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

// assertNoBackslashPersisted walks the metadata root and fails if any persisted
// file or directory name contains a backslash. A backslash byte in a name is the
// #660 trigger that deadlocks the FUSE page-cache layer on open().
func assertNoBackslashPersisted(t *testing.T, root string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		assert.NotContains(t, info.Name(), "\\",
			"metadata entry must not be persisted with a backslash: %s", path)
		return nil
	})
	require.NoError(t, err)
}

// TestWriteFileMetadata_NormalizesBackslashPath guards against issue #660: a
// filename containing a backslash deadlocks the FUSE page-cache layer on open().
// The metadata service must never persist or serve a backslash path, and a read
// using the original backslash form must still resolve.
func TestWriteFileMetadata_NormalizesBackslashPath(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	// Final path component contains a literal backslash (the #660 trigger).
	backslashPath := "movies/Release Name/file.mkv\\file.mkv"
	normalizedPath := "movies/Release Name/file.mkv/file.mkv"

	meta := ms.CreateFileMetadata(
		2048, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "zxcvb09876",
	)
	require.NoError(t, ms.WriteFileMetadata(backslashPath, meta))

	assertNoBackslashPersisted(t, root)

	// The .meta file lives at the normalized location.
	require.FileExists(t, ms.GetMetadataFilePath(normalizedPath))

	// Reading back via the original backslash path resolves to the same entry.
	got, err := ms.ReadFileMetadata(backslashPath)
	require.NoError(t, err)
	require.NotNil(t, got, "metadata written under a backslash path must be readable")
	assert.Equal(t, int64(2048), got.FileSize)

	// And via the normalized forward-slash path.
	gotNorm, err := ms.ReadFileMetadata(normalizedPath)
	require.NoError(t, err)
	require.NotNil(t, gotNorm)
	assert.Equal(t, int64(2048), gotNorm.FileSize)
}

// TestRenameFileMetadata_NormalizesBackslashPath guards the rename path: renaming
// a metadata file to a backslash-bearing virtual path must not persist a backslash
// name (which would re-introduce #660), and the renamed entry must resolve via the
// original backslash form.
func TestRenameFileMetadata_NormalizesBackslashPath(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	srcPath := "movies/old.mkv"
	backslashDst := "movies/Release\\new.mkv"
	normalizedDst := "movies/Release/new.mkv"

	meta := ms.CreateFileMetadata(
		4096, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "rename12345",
	)
	require.NoError(t, ms.WriteFileMetadata(srcPath, meta))

	require.NoError(t, ms.RenameFileMetadata(srcPath, backslashDst))

	assertNoBackslashPersisted(t, root)

	assert.NoFileExists(t, ms.GetMetadataFilePath(srcPath))
	require.FileExists(t, ms.GetMetadataFilePath(normalizedDst))

	// Resolvable via both the backslash and normalized forms.
	got, err := ms.ReadFileMetadata(backslashDst)
	require.NoError(t, err)
	require.NotNil(t, got, "renamed metadata must be readable via the backslash path")
	assert.Equal(t, int64(4096), got.FileSize)
}

// TestDeleteFileMetadata_NormalizesBackslashPath guards the delete path: a call
// with a backslash virtual path must locate and remove the (normalized) persisted
// .meta file rather than silently missing it.
func TestDeleteFileMetadata_NormalizesBackslashPath(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	backslashPath := "movies/Release\\file.mkv"
	normalizedPath := "movies/Release/file.mkv"

	meta := ms.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "delete12345",
	)
	require.NoError(t, ms.WriteFileMetadata(backslashPath, meta))
	require.FileExists(t, ms.GetMetadataFilePath(normalizedPath))

	require.NoError(t, ms.DeleteFileMetadata(context.Background(), backslashPath))

	assert.NoFileExists(t, ms.GetMetadataFilePath(normalizedPath))
}

// TestDeleteDirectory_PurgesCacheForTrailingSeparator covers the cache purge in
// DeleteDirectory, which builds a prefix from the directory path.
//
// Two ways it could miss every key. A trailing separator makes the prefix double
// up ("movies/" becomes "movies//"), and building it from filepath.Separator
// matches nothing on Windows, where the separator is a backslash but cache keys
// are always normalized to forward slashes. Either way the on-disk directory is
// removed while its entries stay in the lite cache, and the next read of one of
// them is served metadata for a file that is gone.
func TestDeleteDirectory_PurgesCacheForTrailingSeparator(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	filePath := "movies/Release/file.mkv"
	meta := ms.CreateFileMetadata(
		2048, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "purge12345",
	)
	require.NoError(t, ms.WriteFileMetadata(filePath, meta))

	// Populate the lite cache so a failed purge is observable.
	lite, err := ms.ReadFileMetadataLite(filePath)
	require.NoError(t, err)
	require.NotNil(t, lite)

	// Trailing backslash: normalization turns it into a trailing forward slash,
	// which is exactly the shape that used to defeat the prefix match.
	require.NoError(t, ms.DeleteDirectory("movies/Release\\"))

	after, err := ms.ReadFileMetadataLite(filePath)
	require.NoError(t, err)
	assert.Nil(t, after, "lite cache must not serve metadata for a deleted directory's files")
}

// TestUpdateFileMetadata_PreservesModifiedAt ensures status and known-holes
// RMW paths do not rewrite ModifiedAt (WebDAV Last-Modified / FUSE mtime).
func TestUpdateFileMetadata_PreservesModifiedAt(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := filepath.Join("movies", "stable_mtime.mkv")
	const fixedModifiedAt int64 = 1_700_000_000

	meta := ms.CreateFileMetadata(
		2048, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "mtime-id",
	)
	meta.ModifiedAt = fixedModifiedAt
	meta.CreatedAt = fixedModifiedAt - 60
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	require.NoError(t, ms.UpdateFileStatus(virtualPath, metapb.FileStatus_FILE_STATUS_DEGRADED))

	afterStatus, err := ms.ReadFileMetadata(virtualPath)
	require.NoError(t, err)
	require.NotNil(t, afterStatus)
	assert.Equal(t, fixedModifiedAt, afterStatus.ModifiedAt)
	assert.Equal(t, metapb.FileStatus_FILE_STATUS_DEGRADED, afterStatus.Status)
	assert.Equal(t, fixedModifiedAt-60, afterStatus.CreatedAt)

	require.NoError(t, ms.AddKnownHoles(virtualPath, []holes.Run{{Start: 10, Count: 2}}, ""))

	afterHoles, err := ms.ReadFileMetadata(virtualPath)
	require.NoError(t, err)
	require.NotNil(t, afterHoles)
	assert.Equal(t, fixedModifiedAt, afterHoles.ModifiedAt)
	require.NotEmpty(t, afterHoles.KnownHoles)
	assert.Equal(t, metapb.FileStatus_FILE_STATUS_DEGRADED, afterHoles.Status)
}

// TestWriteFileMetadata_PinsDiskMtimeToModifiedAt covers the os.Chtimes call
// that follows the atomic rename: os.Rename always stamps the destination with
// mtime=now, which would surface as a fresh timestamp to any host tool reading
// the on-disk mtime rather than the proto.
func TestWriteFileMetadata_PinsDiskMtimeToModifiedAt(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := filepath.Join("movies", "pinned.mkv")
	const fixedModifiedAt int64 = 1_700_000_000

	meta := ms.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	meta.ModifiedAt = fixedModifiedAt
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	info, err := os.Stat(ms.GetMetadataFilePath(virtualPath))
	require.NoError(t, err)
	assert.Equal(t, fixedModifiedAt, info.ModTime().Unix())
}

// TestUpdateFileStatus_UnchangedStatusSkipsWrite pins the fast path that keeps
// restarts cheap: the health worker re-asserts HEALTHY for every known-healthy
// file at startup, and rewriting an identical proto for each one is pure waste.
//
// A sentinel mtime detects the write: WriteFileMetadata always re-stamps the
// file to ModifiedAt, so the sentinel survives only if no write happened.
func TestUpdateFileStatus_UnchangedStatusSkipsWrite(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := filepath.Join("movies", "already_healthy.mkv")
	const fixedModifiedAt int64 = 1_700_000_000

	meta := ms.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	meta.ModifiedAt = fixedModifiedAt
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	metaPath := ms.GetMetadataFilePath(virtualPath)
	sentinel := time.Unix(1_600_000_000, 0)
	require.NoError(t, os.Chtimes(metaPath, sentinel, sentinel))

	// Re-asserting the current status must not touch the file.
	require.NoError(t, ms.UpdateFileStatus(virtualPath, metapb.FileStatus_FILE_STATUS_HEALTHY))

	info, err := os.Stat(metaPath)
	require.NoError(t, err)
	assert.Equal(t, sentinel.Unix(), info.ModTime().Unix(),
		"re-asserting an unchanged status must not rewrite the .meta file")

	// A real transition still writes, restoring the pinned ModifiedAt mtime.
	require.NoError(t, ms.UpdateFileStatus(virtualPath, metapb.FileStatus_FILE_STATUS_DEGRADED))

	info, err = os.Stat(metaPath)
	require.NoError(t, err)
	assert.Equal(t, fixedModifiedAt, info.ModTime().Unix(),
		"a genuine status transition must write and re-pin mtime to ModifiedAt")

	after, err := ms.ReadFileMetadata(virtualPath)
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, metapb.FileStatus_FILE_STATUS_DEGRADED, after.Status)
}

// TestUpdateFileStatus_SkipsWriteOnColdCache ensures the fast path is driven by
// on-disk state, not just a warm liteCache — a restart starts with an empty one.
func TestUpdateFileStatus_SkipsWriteOnColdCache(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := filepath.Join("movies", "cold_cache.mkv")

	meta := ms.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	meta.ModifiedAt = 1_700_000_000
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	ms.liteCache.Purge()

	metaPath := ms.GetMetadataFilePath(virtualPath)
	sentinel := time.Unix(1_600_000_000, 0)
	require.NoError(t, os.Chtimes(metaPath, sentinel, sentinel))

	require.NoError(t, ms.UpdateFileStatus(virtualPath, metapb.FileStatus_FILE_STATUS_HEALTHY))

	info, err := os.Stat(metaPath)
	require.NoError(t, err)
	assert.Equal(t, sentinel.Unix(), info.ModTime().Unix())
}

// TestDirectoryModTime reports the on-disk mtime of the backing metadata
// directory. The kernel already advances it when children are created or
// removed, so no per-child scan is needed to derive a virtual directory mtime.
func TestDirectoryModTime(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	dir := filepath.Join("movies", "ActionMovie")

	// Non-existent directory reports zero, letting callers fall back to an epoch.
	assert.True(t, ms.DirectoryModTime(dir).IsZero())

	meta := ms.CreateFileMetadata(100, "1.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY, nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "")
	meta.ModifiedAt = 1_700_000_100
	require.NoError(t, ms.WriteFileMetadata(filepath.Join(dir, "part1.mkv"), meta))

	onDisk, err := os.Stat(filepath.Join(root, dir))
	require.NoError(t, err)
	assert.Equal(t, onDisk.ModTime(), ms.DirectoryModTime(dir))

	// A file path is not a directory.
	assert.True(t, ms.DirectoryModTime(filepath.Join(dir, "part1.mkv")).IsZero())
}

// TestDirectoryModTime_StableAcrossHealthSweep is the regression guard for the
// original bug: a restart's health sweep must not make directories look freshly
// modified to media scanners that only rescan folders whose mtime changed.
func TestDirectoryModTime_StableAcrossHealthSweep(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	dir := filepath.Join("movies", "ActionMovie")

	for _, name := range []string{"part1.mkv", "part2.mkv", "part3.mkv"} {
		meta := ms.CreateFileMetadata(100, "x.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY, nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "")
		meta.ModifiedAt = 1_700_000_100
		require.NoError(t, ms.WriteFileMetadata(filepath.Join(dir, name), meta))
	}

	before := ms.DirectoryModTime(dir)
	require.False(t, before.IsZero())

	// Simulate the startup sweep re-asserting HEALTHY for every file.
	for _, name := range []string{"part1.mkv", "part2.mkv", "part3.mkv"} {
		require.NoError(t, ms.UpdateFileStatus(filepath.Join(dir, name), metapb.FileStatus_FILE_STATUS_HEALTHY))
	}

	assert.Equal(t, before, ms.DirectoryModTime(dir),
		"a no-op health sweep must not bump the directory mtime")

	// A genuine import advances it (allow at least 10ms for filesystem mtime tick).
	time.Sleep(15 * time.Millisecond)
	newMeta := ms.CreateFileMetadata(300, "3.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY, nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "")
	newMeta.ModifiedAt = 1_700_000_900
	require.NoError(t, ms.WriteFileMetadata(filepath.Join(dir, "part4.mkv"), newMeta))

	assert.True(t, ms.DirectoryModTime(dir).After(before),
		"importing a new file must advance the directory mtime")
}

func TestDeleteDirectory_RefusesToRemoveTheMetadataRoot(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	virtualPath := filepath.Join("movies", "keep.mkv")
	meta := ms.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	// A virtual path resolving to the store itself must never wipe the whole store.
	for _, p := range []string{"", ".", string(filepath.Separator)} {
		err := ms.DeleteDirectory(p)
		assert.ErrorContains(t, err, "safety block", "must refuse virtual path %q", p)
	}

	assert.DirExists(t, root)
	assert.FileExists(t, ms.GetMetadataFilePath(virtualPath), "the store's contents must survive a refused delete")
}

func TestDeleteDirectoryIfEmpty(t *testing.T) {
	newService := func(t *testing.T) (*MetadataService, string) {
		t.Helper()
		root := t.TempDir()
		return NewMetadataService(root), root
	}

	writeMeta := func(t *testing.T, ms *MetadataService, virtualPath string) {
		t.Helper()
		meta := ms.CreateFileMetadata(
			1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
			nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
		)
		require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))
	}

	t.Run("removes empty directory and purges its cached tree", func(t *testing.T) {
		ms, root := newService(t)

		virtualPath := filepath.Join("movies", "Release", "file.mkv")
		writeMeta(t, ms, virtualPath)
		_, err := ms.ReadFileMetadataLite(virtualPath)
		require.NoError(t, err)
		require.NoError(t, os.Remove(ms.GetMetadataFilePath(virtualPath)))

		require.NoError(t, ms.DeleteDirectoryIfEmpty(filepath.Join("movies", "Release")))

		assert.NoDirExists(t, filepath.Join(root, "movies", "Release"))
		_, cached := ms.liteCache.Get(virtualPath)
		assert.False(t, cached, "cached entries under a removed directory must be purged")
	})

	t.Run("preserves a directory that still holds files", func(t *testing.T) {
		ms, root := newService(t)

		virtualPath := filepath.Join("movies", "Release", "sibling.mkv")
		writeMeta(t, ms, virtualPath)
		_, err := ms.ReadFileMetadataLite(virtualPath)
		require.NoError(t, err)

		err = ms.DeleteDirectoryIfEmpty(filepath.Join("movies", "Release"))

		assert.ErrorIs(t, err, ErrDirectoryNotEmpty)
		assert.DirExists(t, filepath.Join(root, "movies", "Release"))
		require.FileExists(t, ms.GetMetadataFilePath(virtualPath))
		_, cached := ms.liteCache.Get(virtualPath)
		assert.True(t, cached, "a preserved directory must keep its cached entries")
	})

	t.Run("missing directory is not an error", func(t *testing.T) {
		ms, _ := newService(t)

		assert.NoError(t, ms.DeleteDirectoryIfEmpty(filepath.Join("movies", "Gone")))
	})

	t.Run("refuses to remove the metadata root", func(t *testing.T) {
		ms, root := newService(t)

		for _, p := range []string{"", ".", string(filepath.Separator)} {
			err := ms.DeleteDirectoryIfEmpty(p)
			assert.ErrorContains(t, err, "safety block", "must refuse virtual path %q", p)
		}
		assert.DirExists(t, root)
	})
}

// TestLongFilename_ReadPathMatchesWritePath covers a behaviour change that comes
// with routing every accessor through metaFilePath: the read side now truncates
// the same way the write side always did.
//
// Before, only WriteFileMetadata and FileExists truncated, so a name over the
// 250-byte budget was written to the truncated path and then looked for at the
// full one, which could not exist. Anything doing stat-then-delete-then-write on
// such a name skipped its pre-delete silently and leaked the store refcount.
func TestLongFilename_ReadPathMatchesWritePath(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	// 300 bytes of base name, comfortably over the 250-byte budget.
	longName := strings.Repeat("a", 300) + ".mkv"
	virtualPath := filepath.Join("movies", longName)

	meta := ms.CreateFileMetadata(
		4096, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "longname123",
	)
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

	// The persisted name is truncated, so the full name is not on disk.
	written := ms.GetMetadataFilePath(virtualPath)
	require.FileExists(t, written)
	assert.Less(t, len(filepath.Base(written)), len(longName),
		"the .meta name should be truncated")

	// Reads resolve to the same place the write went.
	got, err := ms.ReadFileMetadata(virtualPath)
	require.NoError(t, err)
	require.NotNil(t, got, "a name over the truncation budget must still be readable")
	assert.Equal(t, int64(4096), got.FileSize)

	lite, err := ms.ReadFileMetadataLite(virtualPath)
	require.NoError(t, err)
	require.NotNil(t, lite, "the lite read path must truncate the same way")

	// And so does delete, which is what makes stat-then-delete-then-write work.
	require.NoError(t, ms.DeleteFileMetadata(context.Background(), virtualPath))
	assert.NoFileExists(t, written)
}

// TestLongFilename_SharedPrefixCollides documents the hazard the read-side
// truncation introduces, so a future change to truncateFilename has something to
// break. Two names sharing their first 250 bytes and their extension truncate to
// the same .meta path, so the second write is served for a read of the first.
// Previously that read simply missed. Fixing it means a longer budget or a hash
// suffix, which is a change of on-disk layout and out of scope here.
func TestLongFilename_SharedPrefixCollides(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	prefix := strings.Repeat("b", 260)
	first := filepath.Join("movies", prefix+"-one.mkv")
	second := filepath.Join("movies", prefix+"-two.mkv")

	firstMeta := ms.CreateFileMetadata(
		1111, "one.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "collide0001",
	)
	require.NoError(t, ms.WriteFileMetadata(first, firstMeta))

	secondMeta := ms.CreateFileMetadata(
		2222, "two.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "collide0002",
	)
	require.NoError(t, ms.WriteFileMetadata(second, secondMeta))

	require.Equal(t, ms.GetMetadataFilePath(first), ms.GetMetadataFilePath(second),
		"test premise: both names truncate to the same .meta path")

	got, err := ms.ReadFileMetadata(first)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(2222), got.FileSize,
		"known limitation: the second write wins for both names")
}

// TestNormalizeVirtualPath_CanonicalShape pins that one file has exactly one key.
// The metadata layer joins onto rootPath, so a leading slash carries no meaning,
// but it used to survive into the liteCache key: "/movies/x.mkv" and
// "movies/x.mkv" were two entries for the same file, and an eviction under one
// shape could not reach the other.
func TestNormalizeVirtualPath_CanonicalShape(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"movies/x.mkv", "movies/x.mkv"},
		{"/movies/x.mkv", "movies/x.mkv"},
		{"movies//x.mkv", "movies/x.mkv"},
		{"/movies/./x.mkv", "movies/x.mkv"},
		{`movies\x.mkv`, "movies/x.mkv"},
		{`\movies\x.mkv`, "movies/x.mkv"},
		{"movies/sub/../x.mkv", "movies/x.mkv"},
		{"", ""},
		{"/", ""},
		{".", ""},
	} {
		assert.Equal(t, tc.want, normalizeVirtualPath(tc.in), "normalizeVirtualPath(%q)", tc.in)
	}
}

// TestLiteCache_EvictionReachesEntryCachedUnderOtherShape is the consequence of
// the above that actually bites: a writer using one shape must invalidate the
// entry a reader cached under the other.
func TestLiteCache_EvictionReachesEntryCachedUnderOtherShape(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	withSlash := "/movies/shape.mkv"
	withoutSlash := "movies/shape.mkv"

	meta := ms.CreateFileMetadata(
		1024, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "shape123456",
	)
	require.NoError(t, ms.WriteFileMetadata(withSlash, meta))

	// Populate the cache using the leading-slash shape, the way a FUSE reader does.
	cached, err := ms.ReadFileMetadataLite(withSlash)
	require.NoError(t, err)
	require.NotNil(t, cached)

	// Delete using the other shape, the way the migration path does.
	require.NoError(t, ms.DeleteFileMetadata(context.Background(), withoutSlash))

	after, err := ms.ReadFileMetadataLite(withSlash)
	require.NoError(t, err)
	assert.Nil(t, after, "a delete under one path shape must evict the entry cached under the other")
}
