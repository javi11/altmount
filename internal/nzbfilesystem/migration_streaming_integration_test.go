package nzbfilesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migration_streaming_integration_test.go answers the question the unit tests in
// internal/metadata cannot: after a legacy library is migrated to the v3 shared
// NZB store, do the old releases still *stream* — byte for byte, through the
// real read path — rather than merely resolving to equal metadata?
//
// The test writes genuine v1 metadata (MetadataService.WriteFileMetadata with no
// StoreRef emits exactly the pre-v3 on-disk format), streams every file to
// completion through MetadataVirtualFile against a fake NNTP pool, runs the real
// MigrationWorker, then streams the same files again from the rewritten .meta
// files and compares the bytes.

const (
	itSegSize  = 128
	itSegCount = 6
)

// mvfFromMeta builds a MetadataVirtualFile over an already-resolved metadata,
// mirroring newTestMVF but taking its segments from a real .meta read off disk.
func mvfFromMeta(t testing.TB, ctx context.Context, fp *fakepool.Client, name string, m *metapb.FileMetadata) *MetadataVirtualFile {
	t.Helper()
	mvf := &MetadataVirtualFile{
		name: name,
		meta: &fileHandleMeta{
			FileSize:      m.FileSize,
			ModifiedAt:    m.ModifiedAt,
			SourceNzbPath: m.SourceNzbPath,
			Encryption:    m.Encryption,
			AesKey:        m.AesKey,
			AesIv:         m.AesIv,
			SegmentData:   m.SegmentData,
			NestedSources: m.NestedSources,
			KnownHoles:    m.KnownHoles,
		},
		poolManager:      newFakePoolManager(fp),
		ctx:              ctx,
		maxPrefetch:      4,
		originalRangeEnd: -1,
		streamTracker:    noopStreamTracker{},
		streamID:         "integration-stream",
	}
	t.Cleanup(func() { _ = mvf.Close() })
	return mvf
}

// streamAll reads the whole virtual file through ReadAt, the way a player would.
func streamAll(t testing.TB, mvf *MetadataVirtualFile, size int64) []byte {
	t.Helper()
	buf := make([]byte, size)
	n, err := mvf.ReadAt(buf, 0)
	require.NoError(t, err)
	require.EqualValues(t, size, n)
	return buf
}

// slicedExpected is the payload an archive-sliced (partial-offset) file yields.
func slicedExpected() []byte {
	out := make([]byte, 0, 276)
	out = append(out, segments.Payload(2, itSegSize)[40:]...)
	out = append(out, segments.Payload(3, itSegSize)...)
	out = append(out, segments.Payload(4, itSegSize)[:60]...)
	return out
}

// runMigration executes the real worker and waits for it to finish.
func runMigration(t *testing.T, ctx context.Context, ms *metadata.MetadataService, configDir string) *metadata.MigrationResult {
	t.Helper()
	cfg := &config.Config{}
	cfg.Database.Path = filepath.Join(configDir, "altmount.db")
	cfg.Metadata.Migration.DefaultGroup = "alt.binaries.misc"

	worker := metadata.NewMigrationWorker(ms, func() *config.Config { return cfg })
	require.NoError(t, worker.Start(ctx))
	require.Eventually(t, func() bool {
		s := worker.GetStatus()
		return !s.IsRunning && s.LastResult != nil
	}, 15*time.Second, 20*time.Millisecond, "migration should finish")

	result := worker.GetStatus().LastResult
	require.NotNil(t, result)
	require.Equal(t, 0, result.FilesFailed, "no file may fail to migrate: %v", result.Failures)
	return result
}

func TestMigration_LegacyReleaseStillStreamsByteForByte(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	configDir := t.TempDir()

	ms := metadata.NewMetadataService(root)

	fp := fakepool.New()
	configurePoolForFile(fp, itSegCount, itSegSize, fakepool.SegmentBehavior{})

	// Seed a legacy (v1, inline-segment) release: one plain file that will
	// compact into segment_runs, and one archive-sliced file that must keep
	// explicit refs with its partial offsets intact.
	plainPath := filepath.Join("movies", "Plain.mkv")
	slicedPath := filepath.Join("movies", "Sliced.mkv")
	sourceNzb := filepath.Join(configDir, "release.nzb") // absent → synthesized store

	plainSegs := make([]*metapb.SegmentData, itSegCount)
	for i := range plainSegs {
		plainSegs[i] = &metapb.SegmentData{
			Id: segments.MessageID(i), SegmentSize: itSegSize, StartOffset: 0, EndOffset: itSegSize - 1,
		}
	}
	require.NoError(t, ms.WriteFileMetadata(plainPath, &metapb.FileMetadata{
		FileSize:      int64(itSegCount * itSegSize),
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		SourceNzbPath: sourceNzb,
		SegmentData:   plainSegs,
	}))

	slicedSegs := []*metapb.SegmentData{
		{Id: segments.MessageID(2), SegmentSize: itSegSize, StartOffset: 40, EndOffset: itSegSize - 1},
		{Id: segments.MessageID(3), SegmentSize: itSegSize, StartOffset: 0, EndOffset: itSegSize - 1},
		{Id: segments.MessageID(4), SegmentSize: itSegSize, StartOffset: 0, EndOffset: 59},
	}
	require.NoError(t, ms.WriteFileMetadata(slicedPath, &metapb.FileMetadata{
		FileSize:      276,
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		SourceNzbPath: sourceNzb,
		SegmentData:   slicedSegs,
	}))

	// --- Stream both files BEFORE migration.
	preMetaPlain, err := ms.ReadFileMetadata(plainPath)
	require.NoError(t, err)
	preMetaSliced, err := ms.ReadFileMetadata(slicedPath)
	require.NoError(t, err)

	prePlain := streamAll(t, mvfFromMeta(t, ctx, fp, "plain-pre", preMetaPlain), preMetaPlain.FileSize)
	preSliced := streamAll(t, mvfFromMeta(t, ctx, fp, "sliced-pre", preMetaSliced), preMetaSliced.FileSize)

	// Sanity: the pre-migration stream really is the expected content.
	assert.Equal(t, segments.FileBytes(itSegCount, itSegSize), prePlain,
		"pre-migration plain file must stream the injected payload")
	assert.Equal(t, slicedExpected(), preSliced,
		"pre-migration sliced file must stream its partial ranges")

	// --- Run the REAL migration worker, exactly as the panel button does.
	result := runMigration(t, ctx, ms, configDir)
	require.Equal(t, 2, result.FilesMigrated)

	// The on-disk metadata really is v3 now, and the shared store exists.
	rawPlain, err := os.ReadFile(filepath.Join(root, "movies", "Plain.mkv.meta"))
	require.NoError(t, err)
	require.Equal(t, []byte{0x00, 'A', 'M', '3', 0x01}, rawPlain[:5], "meta must carry the v3 magic")

	entries, err := os.ReadDir(filepath.Join(configDir, ".nzbs", "_migrated"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "both files share one .nzbz store")

	// --- Stream both files AFTER migration, resolving through the store.
	postMetaPlain, err := ms.ReadFileMetadata(plainPath)
	require.NoError(t, err)
	postMetaSliced, err := ms.ReadFileMetadata(slicedPath)
	require.NoError(t, err)
	require.NotEmpty(t, postMetaPlain.StoreRef, "migrated meta points at the store")

	postPlain := streamAll(t, mvfFromMeta(t, ctx, fp, "plain-post", postMetaPlain), postMetaPlain.FileSize)
	postSliced := streamAll(t, mvfFromMeta(t, ctx, fp, "sliced-post", postMetaSliced), postMetaSliced.FileSize)

	// The whole point: old releases stream identically after migration.
	assert.Equal(t, prePlain, postPlain, "plain file must stream identically after migration")
	assert.Equal(t, preSliced, postSliced, "archive-sliced file must stream identically after migration")
	assert.Equal(t, segments.FileBytes(itSegCount, itSegSize), postPlain)
	assert.Equal(t, slicedExpected(), postSliced)
}

func TestMigration_PartialRangeReadsMatchAfterMigration(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	configDir := t.TempDir()
	ms := metadata.NewMetadataService(root)

	fp := fakepool.New()
	configurePoolForFile(fp, itSegCount, itSegSize, fakepool.SegmentBehavior{})

	vpath := filepath.Join("movies", "Seek.mkv")
	segs := make([]*metapb.SegmentData, itSegCount)
	for i := range segs {
		segs[i] = &metapb.SegmentData{
			Id: segments.MessageID(i), SegmentSize: itSegSize, StartOffset: 0, EndOffset: itSegSize - 1,
		}
	}
	require.NoError(t, ms.WriteFileMetadata(vpath, &metapb.FileMetadata{
		FileSize:      int64(itSegCount * itSegSize),
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		SourceNzbPath: filepath.Join(configDir, "seek.nzb"),
		SegmentData:   segs,
	}))

	// Mid-file seeks are where a wrong flat index would surface first.
	offsets := []int64{0, 1, 127, 128, 200, 511, 640}
	readAtOffset := func(m *metapb.FileMetadata, off int64) []byte {
		mvf := mvfFromMeta(t, ctx, fp, "seek", m)
		buf := make([]byte, 64)
		n, err := mvf.ReadAt(buf, off)
		require.NoError(t, err)
		return buf[:n]
	}

	preMeta, err := ms.ReadFileMetadata(vpath)
	require.NoError(t, err)
	before := make(map[int64][]byte, len(offsets))
	for _, off := range offsets {
		before[off] = readAtOffset(preMeta, off)
	}

	_ = runMigration(t, ctx, ms, configDir)

	postMeta, err := ms.ReadFileMetadata(vpath)
	require.NoError(t, err)
	require.NotEmpty(t, postMeta.StoreRef)

	full := segments.FileBytes(itSegCount, itSegSize)
	for _, off := range offsets {
		got := readAtOffset(postMeta, off)
		assert.Equal(t, before[off], got, "read at offset %d must be unchanged by migration", off)
		assert.Equal(t, full[off:off+int64(len(got))], got, "read at offset %d must match the source payload", off)
	}
}
