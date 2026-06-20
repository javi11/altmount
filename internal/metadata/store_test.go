package metadata

import (
	"path/filepath"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func sampleStore() *metapb.NzbStore {
	return &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Subject: "Movie.mkv yEnc (1/2)", Poster: "p@x", Date: 1000, Groups: []string{"a.b.test"},
			Segments: []*metapb.NzbSeg{{Id: "m1@x", Number: 1, Bytes: 700000}, {Id: "m2@x", Number: 2, Bytes: 500000}}},
		{Subject: "Movie.par2 yEnc (1/1)", Poster: "p@x", Date: 1000, Groups: []string{"a.b.test"},
			Segments: []*metapb.NzbSeg{{Id: "p1@x", Number: 1, Bytes: 4096}}},
	}}
}

func TestStoreService_WriteRead(t *testing.T) {
	ss := NewStoreService(t.TempDir())
	ref := filepath.Join(t.TempDir(), "rel.nzbz")
	orig := sampleStore()
	require.NoError(t, ss.WriteStore(ref, orig))

	got, err := ss.ReadStore(ref)
	require.NoError(t, err)
	require.True(t, proto.Equal(orig, got))

	// flat index: file0 seg0, file0 seg1, file1 seg0
	flat := FlatSegments(got)
	require.Len(t, flat, 3)
	assert.Equal(t, "m1@x", flat[0].Id)
	assert.Equal(t, "m2@x", flat[1].Id)
	assert.Equal(t, "p1@x", flat[2].Id)
}

func TestResolveRefs(t *testing.T) {
	store := sampleStore()
	flat := FlatSegments(store)
	refs := []*metapb.SegmentRef{
		{StoreIndex: 0, StartOffset: 0, EndOffset: 699999},
		{StoreIndex: 2, StartOffset: 10, EndOffset: 4095},
	}
	segs, err := resolveRefs(flat, refs)
	require.NoError(t, err)
	require.Len(t, segs, 2)
	assert.Equal(t, "m1@x", segs[0].Id)
	assert.Equal(t, int64(700000), segs[0].SegmentSize)
	assert.Equal(t, int64(0), segs[0].StartOffset)
	assert.Equal(t, "p1@x", segs[1].Id)
	assert.Equal(t, int64(4096), segs[1].SegmentSize)
	assert.Equal(t, int64(10), segs[1].StartOffset)

	_, err = resolveRefs(flat, []*metapb.SegmentRef{{StoreIndex: 99}})
	assert.Error(t, err, "out-of-range index must error")
}
