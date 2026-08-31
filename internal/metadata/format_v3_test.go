package metadata

import (
	"os"
	"path/filepath"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestV3_WriteRead_ResolvesSegments(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	storeRef := filepath.Join(t.TempDir(), "rel.nzbz")

	store := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Subject: "Movie.mkv", Poster: "p", Date: 1, Groups: []string{"g"},
			Segments: []*metapb.NzbSeg{
				{Id: "a@n", Number: 1, Bytes: 100},
				{Id: "b@n", Number: 2, Bytes: 100},
			}},
	}}
	require.NoError(t, ms.Store().WriteStore(storeRef, store))

	meta := &metapb.FileMetadata{
		FileSize: 200,
		Status:   metapb.FileStatus_FILE_STATUS_HEALTHY,
		StoreRef: storeRef,
		SegmentRefs: []*metapb.SegmentRef{
			{StoreIndex: 0, StartOffset: 0, EndOffset: 99},
			{StoreIndex: 1, StartOffset: 0, EndOffset: 99},
		},
	}
	vpath := filepath.Join("movies", "Movie.mkv")
	require.NoError(t, ms.WriteFileMetadata(vpath, meta))

	got, err := ms.ReadFileMetadata(vpath)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.SegmentData, 2, "refs must resolve to SegmentData for consumers")
	assert.Equal(t, "a@n", got.SegmentData[0].Id)
	assert.Equal(t, int64(100), got.SegmentData[0].SegmentSize)
	assert.Equal(t, storeRef, got.StoreRef)
}

func TestV3_ReadsV1Legacy(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	v1 := &metapb.FileMetadata{
		FileSize: 10,
		Status:   metapb.FileStatus_FILE_STATUS_HEALTHY,
		SegmentData: []*metapb.SegmentData{
			{Id: "x@n", SegmentSize: 10, EndOffset: 9},
		},
	}
	dir := filepath.Join(root, "movies")
	require.NoError(t, os.MkdirAll(dir, 0755))
	raw, err := proto.Marshal(v1)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "legacy.mkv.meta"), raw, 0644))

	got, err := ms.ReadFileMetadata(filepath.Join("movies", "legacy.mkv"))
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.SegmentData, 1)
	assert.Equal(t, "x@n", got.SegmentData[0].Id)
}

func TestV3_MetaFileHasMagicPrefix(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	storeRef := filepath.Join(t.TempDir(), "rel.nzbz")

	store := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Segments: []*metapb.NzbSeg{{Id: "s@n", Number: 1, Bytes: 50}}},
	}}
	require.NoError(t, ms.Store().WriteStore(storeRef, store))

	meta := &metapb.FileMetadata{
		FileSize: 50,
		StoreRef: storeRef,
		SegmentRefs: []*metapb.SegmentRef{
			{StoreIndex: 0, StartOffset: 0, EndOffset: 49},
		},
	}
	vpath := "test/file.mkv"
	require.NoError(t, ms.WriteFileMetadata(vpath, meta))

	// Raw on-disk bytes should start with v3 magic
	rawPath := ms.GetMetadataFilePath(vpath)
	raw, err := os.ReadFile(rawPath)
	require.NoError(t, err)
	assert.True(t, isV3Meta(raw), "on-disk .meta must start with v3 magic bytes")
	assert.Equal(t, metaMagicV3, raw[:len(metaMagicV3)])
}

func TestV3_LiteRead_SkipsMagic(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	storeRef := filepath.Join(t.TempDir(), "rel.nzbz")

	store := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Segments: []*metapb.NzbSeg{{Id: "s@n", Number: 1, Bytes: 50}}},
	}}
	require.NoError(t, ms.Store().WriteStore(storeRef, store))

	meta := &metapb.FileMetadata{
		FileSize: 12345,
		Status:   metapb.FileStatus_FILE_STATUS_HEALTHY,
		StoreRef: storeRef,
		SegmentRefs: []*metapb.SegmentRef{
			{StoreIndex: 0, StartOffset: 0, EndOffset: 49},
		},
	}
	vpath := "test/lite.mkv"
	require.NoError(t, ms.WriteFileMetadata(vpath, meta))
	ms.liteCache.Purge()

	lite, err := ms.ReadFileMetadataLite(vpath)
	require.NoError(t, err)
	require.NotNil(t, lite)
	assert.Equal(t, int64(12345), lite.FileSize)
	assert.Equal(t, metapb.FileStatus_FILE_STATUS_HEALTHY, lite.Status)
}
