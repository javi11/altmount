package nzb

import (
	"bytes"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/nzbparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildNZB_RoundTrip(t *testing.T) {
	store := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{
			Subject: `[1/2] "Movie.mkv" yEnc (1/2)`,
			Poster:  "poster@example.com",
			Date:    1700000000,
			Groups:  []string{"alt.binaries.test", "alt.binaries.x"},
			Segments: []*metapb.NzbSeg{
				{Id: "abc@news", Number: 1, Bytes: 700000},
				{Id: "def@news", Number: 2, Bytes: 500000},
			},
		},
	}}

	xml := BuildNZB(store)
	require.NotEmpty(t, xml)
	require.Contains(t, string(xml), "newzbin")

	parsed, err := nzbparser.Parse(bytes.NewReader(xml))
	require.NoError(t, err)
	require.Len(t, parsed.Files, 1)
	f := parsed.Files[0]
	assert.Equal(t, "poster@example.com", f.Poster)
	assert.Equal(t, 1700000000, f.Date)
	assert.ElementsMatch(t, []string{"alt.binaries.test", "alt.binaries.x"}, f.Groups)
	require.Len(t, f.Segments, 2)
	assert.Equal(t, "abc@news", f.Segments[0].ID)
	assert.Equal(t, 1, f.Segments[0].Number)
	assert.Equal(t, 700000, f.Segments[0].Bytes)
	assert.Equal(t, "def@news", f.Segments[1].ID)
	assert.Equal(t, 2, f.Segments[1].Number)
	assert.Equal(t, 500000, f.Segments[1].Bytes)
}

func TestBuildNZB_Empty(t *testing.T) {
	xml := BuildNZB(&metapb.NzbStore{})
	require.NotEmpty(t, xml)
	parsed, err := nzbparser.Parse(bytes.NewReader(xml))
	require.NoError(t, err)
	assert.Empty(t, parsed.Files)
}
