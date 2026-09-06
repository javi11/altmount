package metadata

import (
	"os"
	"path/filepath"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeLegacyMeta writes a v1 (inline-segment, no magic) meta at virtualPath.
func writeLegacyMeta(t *testing.T, ms *MetadataService, virtualPath, sourceNzb string, ids ...string) {
	t.Helper()
	segs := make([]*metapb.SegmentData, 0, len(ids))
	for _, id := range ids {
		segs = append(segs, &metapb.SegmentData{Id: id, SegmentSize: 100, StartOffset: 0, EndOffset: 99})
	}
	meta := &metapb.FileMetadata{
		FileSize:      int64(100 * len(ids)),
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		SourceNzbPath: sourceNzb,
		SegmentData:   segs,
	}
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))
}

func TestScanLegacyMetas_GroupsBySourceNzbPath(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	writeLegacyMeta(t, ms, filepath.Join("movies", "A.mkv"), "/nzbs/rel.nzb", "a1@n", "a2@n")
	writeLegacyMeta(t, ms, filepath.Join("movies", "B.mkv"), "/nzbs/rel.nzb", "b1@n")
	writeLegacyMeta(t, ms, filepath.Join("other", "C.mkv"), "", "c1@n")

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 2, "two releases: one keyed by source nzb, one by parent dir")

	byKey := map[string][]LegacyMeta{}
	for _, g := range groups {
		byKey[g.Key] = g.Files
	}
	require.Len(t, byKey["/nzbs/rel.nzb"], 2)
	assert.Equal(t, filepath.Join("movies", "A.mkv"), byKey["/nzbs/rel.nzb"][0].VirtualPath)
	assert.Equal(t, filepath.Join("movies", "B.mkv"), byKey["/nzbs/rel.nzb"][1].VirtualPath)

	dirKey := filepath.Join(root, "other")
	require.Len(t, byKey[dirKey], 1, "empty source_nzb_path falls back to the parent directory")
	assert.Positive(t, byKey[dirKey][0].SizeBytes)

	// The scan must not retain full protos: that is the whole-library
	// allocation pattern ReadFileMetadataLite was introduced to avoid.
	for _, g := range groups {
		for _, lm := range g.Files {
			assert.Nil(t, lm.Meta, "scan must not retain segment data for %s", lm.VirtualPath)
		}
	}
}

func TestLoadGroupMetas_HydratesOneGroup(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	writeLegacyMeta(t, ms, filepath.Join("movies", "A.mkv"), "/nzbs/rel.nzb", "a1@n", "a2@n")
	writeLegacyMeta(t, ms, filepath.Join("movies", "B.mkv"), "/nzbs/rel.nzb", "b1@n")

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1)

	loaded, err := ms.LoadGroupMetas(groups[0])
	require.NoError(t, err)
	require.Len(t, loaded.Files, 2)
	require.NotNil(t, loaded.Files[0].Meta)
	assert.Len(t, loaded.Files[0].Meta.SegmentData, 2)
	assert.Len(t, loaded.Files[1].Meta.SegmentData, 1)
	assert.Equal(t, groups[0].Key, loaded.Key)
}

func TestScanLegacyMetas_SkipsV3Metas(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	storeRef := filepath.Join(t.TempDir(), "rel.nzbz")

	store := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Subject: "V3.mkv", Segments: []*metapb.NzbSeg{{Id: "v1@n", Number: 1, Bytes: 100}}},
	}}
	require.NoError(t, ms.Store().WriteStore(storeRef, store))
	require.NoError(t, ms.WriteFileMetadata(filepath.Join("movies", "V3.mkv"), &metapb.FileMetadata{
		FileSize: 100,
		StoreRef: storeRef,
		SegmentRefs: []*metapb.SegmentRef{
			{StoreIndex: 0, StartOffset: 0, EndOffset: 99, DecodedBytes: 100},
		},
	}))
	writeLegacyMeta(t, ms, filepath.Join("movies", "Old.mkv"), "/nzbs/old.nzb", "o1@n")

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1, "already-migrated metas are skipped")
	assert.Equal(t, "/nzbs/old.nzb", groups[0].Key)

	// Sanity: a non-.meta file in the tree is ignored.
	require.NoError(t, os.WriteFile(filepath.Join(root, "movies", "stray.txt"), []byte("x"), 0644))
	groups, err = ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1)
}
