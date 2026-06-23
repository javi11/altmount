package parser

import (
	"sort"
	"testing"

	"github.com/javi11/nzbparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildStore(t *testing.T) {
	nzb := &nzbparser.Nzb{
		Files: nzbparser.NzbFiles{
			{Subject: "Movie.mkv (1/2)", Poster: "p@x", Date: 1000, Groups: []string{"a.b.test"},
				Segments: nzbparser.NzbSegments{{ID: "m1@x", Number: 1, Bytes: 700000}, {ID: "m2@x", Number: 2, Bytes: 500000}}},
			{Subject: "Movie.par2 (1/1)", Poster: "p@x", Date: 1000, Groups: []string{"a.b.test"},
				Segments: nzbparser.NzbSegments{{ID: "p1@x", Number: 1, Bytes: 4096}}},
		},
	}
	// Sort segments as the parser would
	for i := range nzb.Files {
		sort.Sort(nzb.Files[i].Segments)
	}

	store, index := BuildStore(nzb)
	require.NotNil(t, store)
	require.Len(t, store.Files, 2)
	assert.Equal(t, "Movie.mkv (1/2)", store.Files[0].Subject)
	assert.Equal(t, "p@x", store.Files[0].Poster)
	assert.EqualValues(t, 1000, store.Files[0].Date)
	assert.Equal(t, []string{"a.b.test"}, store.Files[0].Groups)
	require.Len(t, store.Files[0].Segments, 2)
	assert.Equal(t, "m1@x", store.Files[0].Segments[0].Id)
	assert.EqualValues(t, 700000, store.Files[0].Segments[0].Bytes)

	// Flat index: file0 seg0=0, file0 seg1=1, file1 seg0=2
	assert.EqualValues(t, 0, index["m1@x"])
	assert.EqualValues(t, 1, index["m2@x"])
	assert.EqualValues(t, 2, index["p1@x"])
}
