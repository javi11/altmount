package metadata

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/nzb"
	"github.com/javi11/nzbparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func legacyMeta(virtualPath string, meta *metapb.FileMetadata) LegacyMeta {
	return LegacyMeta{
		MetaPath:    virtualPath + ".meta",
		VirtualPath: virtualPath,
		SizeBytes:   1,
		Meta:        meta,
	}
}

func TestSynthesizeStore_FlatIndexAndDedup(t *testing.T) {
	a := &metapb.FileMetadata{
		CreatedAt: 111,
		SegmentData: []*metapb.SegmentData{
			{Id: "s1@n", SegmentSize: 100, EndOffset: 99},
			{Id: "s2@n", SegmentSize: 100, EndOffset: 99},
		},
		Par2Files: []*metapb.Par2FileReference{
			{Filename: "rel.par2", SegmentData: []*metapb.SegmentData{{Id: "p1@n", SegmentSize: 50, EndOffset: 49}}},
		},
	}
	b := &metapb.FileMetadata{
		CreatedAt: 222,
		SegmentData: []*metapb.SegmentData{
			{Id: "s2@n", SegmentSize: 100, EndOffset: 99},
			{Id: "s3@n", SegmentSize: 70, EndOffset: 69},
		},
	}

	store, index, err := synthesizeStore(
		[]LegacyMeta{legacyMeta("movies/A.mkv", a), legacyMeta("movies/B.mkv", b)},
		"alt.binaries.misc",
	)
	require.NoError(t, err)
	require.Len(t, store.Files, 2, "one NzbFileEntry per contributing meta")

	assert.Equal(t, "A.mkv", store.Files[0].Subject)
	assert.Equal(t, []string{"alt.binaries.misc"}, store.Files[0].Groups)
	assert.EqualValues(t, 111, store.Files[0].Date)

	assert.EqualValues(t, 0, index["s1@n"])
	assert.EqualValues(t, 1, index["s2@n"])
	assert.EqualValues(t, 2, index["p1@n"])
	assert.EqualValues(t, 3, index["s3@n"])
	require.Len(t, store.Files[1].Segments, 1, "duplicate ids are not stored twice")
	assert.Equal(t, "s3@n", store.Files[1].Segments[0].Id)

	assert.EqualValues(t, 100, store.Files[0].Segments[0].Bytes)
	assert.EqualValues(t, 70, store.Files[1].Segments[0].Bytes)

	assert.EqualValues(t, 1, store.Files[0].Segments[0].Number)
	assert.EqualValues(t, 2, store.Files[0].Segments[1].Number)
}

func TestSynthesizeStore_ExpandsSharedOuterSources(t *testing.T) {
	m := &metapb.FileMetadata{
		SharedOuterSources: []*metapb.NestedSegmentSource{
			{Segments: []*metapb.SegmentData{{Id: "outer@n", SegmentSize: 500, EndOffset: 499}}, InnerVolumeSize: 500},
		},
		NestedSources: []*metapb.NestedSegmentSource{
			{SharedOuterSourceIndex: 1, InnerOffset: 0, InnerLength: 250},
			{SharedOuterSourceIndex: 1, InnerOffset: 250, InnerLength: 250},
		},
	}

	_, index, err := synthesizeStore([]LegacyMeta{legacyMeta("movies/BD.m2ts", m)}, "alt.binaries.misc")
	require.NoError(t, err)
	assert.EqualValues(t, 0, index["outer@n"], "shared outer segments are expanded and indexed")
	assert.Len(t, index, 1)
}

func TestSynthesizeStore_RejectsEmptySegmentID(t *testing.T) {
	m := &metapb.FileMetadata{SegmentData: []*metapb.SegmentData{{Id: "", SegmentSize: 10, EndOffset: 9}}}
	_, _, err := synthesizeStore([]LegacyMeta{legacyMeta("movies/Bad.mkv", m)}, "alt.binaries.misc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty segment id")
}

func TestStoreHashPath_StableAndContentAddressed(t *testing.T) {
	s1 := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Segments: []*metapb.NzbSeg{{Id: "x@n"}, {Id: "y@n"}}},
	}}
	s2 := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Segments: []*metapb.NzbSeg{{Id: "x@n"}}},
	}}

	p1 := storeHashPath("/store", "/nzbs/My Release [2024].nzb", s1)
	assert.Equal(t, p1, storeHashPath("/store", "/nzbs/My Release [2024].nzb", s1), "stable across calls")
	assert.NotEqual(t, p1, storeHashPath("/store", "/nzbs/My Release [2024].nzb", s2), "different segments, different path")

	base := filepath.Base(p1)
	assert.True(t, strings.HasSuffix(base, ".nzbz"))
	assert.True(t, strings.HasPrefix(base, "My_Release__2024_-"), "unsafe characters are replaced, got %q", base)
	assert.NotContains(t, base, "/")
}

const testNZB = `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <file poster="poster@example.com" date="1700000000" subject="[1/1] &quot;A.mkv&quot; yEnc (1/2)">
    <groups><group>alt.binaries.real</group></groups>
    <segments>
      <segment bytes="140" number="1">s1@n</segment>
      <segment bytes="140" number="2">s2@n</segment>
    </segments>
  </file>
</nzb>
`

func TestBuildGroupStore_FaithfulWhenSourceNzbExists(t *testing.T) {
	dir := t.TempDir()
	nzbPath := filepath.Join(dir, "rel.nzb")
	require.NoError(t, os.WriteFile(nzbPath, []byte(testNZB), 0644))

	g := LegacyGroup{Key: nzbPath, Files: []LegacyMeta{legacyMeta("movies/A.mkv", &metapb.FileMetadata{
		SegmentData: []*metapb.SegmentData{
			{Id: "s1@n", SegmentSize: 100, EndOffset: 99},
			{Id: "s2@n", SegmentSize: 100, EndOffset: 99},
		},
	})}}

	store, index, faithful, err := buildGroupStore(g, "alt.binaries.misc")
	require.NoError(t, err)
	assert.True(t, faithful)
	require.Len(t, store.Files, 1)
	assert.Equal(t, []string{"alt.binaries.real"}, store.Files[0].Groups, "real groups survive")
	assert.Equal(t, "poster@example.com", store.Files[0].Poster)
	assert.EqualValues(t, 0, index["s1@n"])
	assert.EqualValues(t, 1, index["s2@n"])

	// A faithful store regenerates an NZB that parses back with its group intact.
	regenerated := nzb.BuildNZB(store)
	reparsed, parseErr := nzbparser.Parse(bytes.NewReader(regenerated))
	require.NoError(t, parseErr)
	require.Len(t, reparsed.Files, 1)
	assert.Equal(t, []string{"alt.binaries.real"}, reparsed.Files[0].Groups)
	require.Len(t, reparsed.Files[0].Segments, 2)
}

func TestBuildGroupStore_FallsBackWhenNzbMissing(t *testing.T) {
	g := LegacyGroup{Key: "/nzbs/gone.nzb", Files: []LegacyMeta{legacyMeta("movies/A.mkv", &metapb.FileMetadata{
		SegmentData: []*metapb.SegmentData{{Id: "s1@n", SegmentSize: 100, EndOffset: 99}},
	})}}

	store, index, faithful, err := buildGroupStore(g, "alt.binaries.misc")
	require.NoError(t, err)
	assert.False(t, faithful)
	require.Len(t, store.Files, 1)
	assert.Equal(t, []string{"alt.binaries.misc"}, store.Files[0].Groups)
	assert.EqualValues(t, 0, index["s1@n"])
}

func TestBuildGroupStore_FallsBackWhenNzbDoesNotCoverSegments(t *testing.T) {
	dir := t.TempDir()
	nzbPath := filepath.Join(dir, "rel.nzb")
	require.NoError(t, os.WriteFile(nzbPath, []byte(testNZB), 0644))

	g := LegacyGroup{Key: nzbPath, Files: []LegacyMeta{legacyMeta("movies/A.mkv", &metapb.FileMetadata{
		SegmentData: []*metapb.SegmentData{
			{Id: "s1@n", SegmentSize: 100, EndOffset: 99},
			{Id: "unknown@n", SegmentSize: 100, EndOffset: 99},
		},
	})}}

	store, index, faithful, err := buildGroupStore(g, "alt.binaries.misc")
	require.NoError(t, err)
	assert.False(t, faithful, "a mismatched NZB must not be trusted")
	assert.Equal(t, []string{"alt.binaries.misc"}, store.Files[0].Groups)
	assert.Contains(t, index, "unknown@n")
}
