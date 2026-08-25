package metadata

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// readResolved returns the SegmentData a consumer would see for a virtual path.
func readResolved(t *testing.T, ms *MetadataService, virtualPath string) *metapb.FileMetadata {
	t.Helper()
	got, err := ms.ReadFileMetadata(virtualPath)
	require.NoError(t, err)
	require.NotNil(t, got)
	return got
}

func TestMigrateGroup_PreservesResolvedSegments(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)

	pathA := filepath.Join("movies", "A.mkv")
	pathB := filepath.Join("movies", "B.mkv")
	metaA := &metapb.FileMetadata{
		FileSize:      300,
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		SourceNzbPath: "/nzbs/rel.nzb",
		CreatedAt:     10,
		ModifiedAt:    20,
		ReleaseDate:   30,
		KnownHoles:    []*metapb.HoleRun{{StartSegment: 1, Count: 1}},
		SegmentData: []*metapb.SegmentData{
			{Id: "s1@n", SegmentSize: 100, StartOffset: 0, EndOffset: 99},
			{Id: "s2@n", SegmentSize: 100, StartOffset: 0, EndOffset: 99},
			{Id: "s3@n", SegmentSize: 100, StartOffset: 0, EndOffset: 99},
		},
		Par2Files: []*metapb.Par2FileReference{
			{Filename: "rel.par2", FileSize: 50, SegmentData: []*metapb.SegmentData{
				{Id: "p1@n", SegmentSize: 50, StartOffset: 0, EndOffset: 49},
			}},
		},
	}
	metaB := &metapb.FileMetadata{
		FileSize:      120,
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		SourceNzbPath: "/nzbs/rel.nzb",
		SegmentData: []*metapb.SegmentData{
			{Id: "s2@n", SegmentSize: 100, StartOffset: 40, EndOffset: 99},
			{Id: "s3@n", SegmentSize: 100, StartOffset: 0, EndOffset: 59},
		},
	}
	require.NoError(t, ms.WriteFileMetadata(pathA, metaA))
	require.NoError(t, ms.WriteFileMetadata(pathB, metaB))

	beforeA := proto.Clone(readResolved(t, ms, pathA)).(*metapb.FileMetadata)
	beforeB := proto.Clone(readResolved(t, ms, pathB)).(*metapb.FileMetadata)

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1)

	res, err := ms.MigrateGroup(context.Background(), groups[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)
	assert.Equal(t, 2, res.FilesMigrated)
	assert.Equal(t, 0, res.FilesFailed)
	assert.False(t, res.Faithful, "no source nzb on disk")
	assert.FileExists(t, res.StoreRef)

	afterA := readResolved(t, ms, pathA)
	afterB := readResolved(t, ms, pathB)

	assert.Equal(t, res.StoreRef, afterA.StoreRef, "meta now points at the shared store")
	require.Len(t, afterA.SegmentData, len(beforeA.SegmentData))
	for i := range beforeA.SegmentData {
		assert.Equal(t, beforeA.SegmentData[i].Id, afterA.SegmentData[i].Id)
		assert.Equal(t, beforeA.SegmentData[i].SegmentSize, afterA.SegmentData[i].SegmentSize)
		assert.Equal(t, beforeA.SegmentData[i].StartOffset, afterA.SegmentData[i].StartOffset)
		assert.Equal(t, beforeA.SegmentData[i].EndOffset, afterA.SegmentData[i].EndOffset)
	}
	require.Len(t, afterA.Par2Files, 1)
	require.Len(t, afterA.Par2Files[0].SegmentData, 1)
	assert.Equal(t, "p1@n", afterA.Par2Files[0].SegmentData[0].Id)

	require.Len(t, afterB.SegmentData, 2)
	assert.EqualValues(t, 40, afterB.SegmentData[0].StartOffset, "archive slicing offsets survive")
	assert.EqualValues(t, 59, afterB.SegmentData[1].EndOffset)
	assert.Equal(t, beforeB.FileSize, afterB.FileSize)

	assert.Equal(t, beforeA.FileSize, afterA.FileSize)
	assert.Equal(t, beforeA.ReleaseDate, afterA.ReleaseDate)
	assert.Equal(t, metapb.FileStatus_FILE_STATUS_HEALTHY, afterA.Status)
	require.Len(t, afterA.KnownHoles, 1)
	assert.EqualValues(t, 1, afterA.KnownHoles[0].StartSegment)
}

func TestMigrateGroup_CompactsIntoSegmentRuns(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)

	segs := make([]*metapb.SegmentData, 0, 10)
	for i := 0; i < 10; i++ {
		segs = append(segs, &metapb.SegmentData{
			Id: string(rune('a'+i)) + "@n", SegmentSize: 100, StartOffset: 0, EndOffset: 99,
		})
	}
	vpath := filepath.Join("movies", "Plain.mkv")
	require.NoError(t, ms.WriteFileMetadata(vpath, &metapb.FileMetadata{
		FileSize: 1000, SourceNzbPath: "/nzbs/plain.nzb", SegmentData: segs,
	}))

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	_, err = ms.MigrateGroup(context.Background(), groups[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(root, "movies", "Plain.mkv.meta"))
	require.NoError(t, err)
	require.True(t, isV3Meta(raw))
	var stored metapb.FileMetadata
	require.NoError(t, proto.Unmarshal(raw[5:], &stored))
	assert.Len(t, stored.SegmentRefs, 0, "no explicit refs for a uniform file")
	require.Len(t, stored.SegmentRuns, 1)
	assert.EqualValues(t, 10, stored.SegmentRuns[0].Count)
	assert.Empty(t, stored.SegmentData, "inline segments are gone")
}

func TestMigrateGroup_IsIdempotentAndSkipsMigratedFiles(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)

	vpath := filepath.Join("movies", "A.mkv")
	require.NoError(t, ms.WriteFileMetadata(vpath, &metapb.FileMetadata{
		FileSize: 100, SourceNzbPath: "/nzbs/rel.nzb",
		SegmentData: []*metapb.SegmentData{{Id: "s1@n", SegmentSize: 100, EndOffset: 99}},
	}))

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	first, err := ms.MigrateGroup(context.Background(), groups[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)

	groups, err = ms.ScanLegacyMetas()
	require.NoError(t, err)
	assert.Empty(t, groups, "migrated metas are not rescanned")
	assert.FileExists(t, first.StoreRef)
}

// countingRefCounter records IncStoreRef calls per store path.
type countingRefCounter struct {
	mu   sync.Mutex
	inc  map[string]int
	decs map[string]int
}

func newCountingRefCounter() *countingRefCounter {
	return &countingRefCounter{inc: map[string]int{}, decs: map[string]int{}}
}

func (c *countingRefCounter) IncStoreRef(_ context.Context, storePath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inc[storePath]++
	return nil
}

func (c *countingRefCounter) DecStoreRef(_ context.Context, storePath string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.decs[storePath]++
	return int64(c.inc[storePath] - c.decs[storePath]), nil
}

func TestMigrateGroup_RefCountMatchesMigratedFiles(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)
	counter := newCountingRefCounter()
	ms.SetStoreRefCounter(counter)

	for _, name := range []string{"A.mkv", "B.mkv", "C.mkv"} {
		require.NoError(t, ms.WriteFileMetadata(filepath.Join("movies", name), &metapb.FileMetadata{
			FileSize: 100, SourceNzbPath: "/nzbs/rel.nzb",
			SegmentData: []*metapb.SegmentData{{Id: name + "@n", SegmentSize: 100, EndOffset: 99}},
		}))
	}

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1)

	res, err := ms.MigrateGroup(context.Background(), groups[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)
	assert.Equal(t, 3, res.FilesMigrated)

	counter.mu.Lock()
	defer counter.mu.Unlock()
	assert.Equal(t, 3, counter.inc[res.StoreRef], "one reference per migrated meta")
}

func TestMigrateGroup_PartialRunLeavesFirstStoreIntact(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)

	pathA := filepath.Join("movies", "A.mkv")
	pathB := filepath.Join("movies", "B.mkv")
	for _, p := range []struct{ path, id string }{{pathA, "a@n"}, {pathB, "b@n"}} {
		require.NoError(t, ms.WriteFileMetadata(p.path, &metapb.FileMetadata{
			FileSize: 100, SourceNzbPath: "/nzbs/rel.nzb",
			SegmentData: []*metapb.SegmentData{{Id: p.id, SegmentSize: 100, EndOffset: 99}},
		}))
	}

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups[0].Files, 2)

	firstOnly := LegacyGroup{Key: groups[0].Key, Files: groups[0].Files[:1]}
	firstRes, err := ms.MigrateGroup(context.Background(), firstOnly, storeDir, "alt.binaries.misc")
	require.NoError(t, err)
	require.Equal(t, 1, firstRes.FilesMigrated)

	remaining, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Len(t, remaining[0].Files, 1, "only B is left")

	secondRes, err := ms.MigrateGroup(context.Background(), remaining[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)
	assert.Equal(t, 1, secondRes.FilesMigrated)
	assert.NotEqual(t, firstRes.StoreRef, secondRes.StoreRef,
		"a different segment set must not reuse the first store path")
	assert.FileExists(t, firstRes.StoreRef, "the first store is untouched")

	gotA := readResolved(t, ms, pathA)
	gotB := readResolved(t, ms, pathB)
	require.Len(t, gotA.SegmentData, 1)
	require.Len(t, gotB.SegmentData, 1)
	assert.Equal(t, "a@n", gotA.SegmentData[0].Id)
	assert.Equal(t, "b@n", gotB.SegmentData[0].Id)
}
