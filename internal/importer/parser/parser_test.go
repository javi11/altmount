package parser

import (
	"context"
	"strings"
	"testing"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nzbparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockPoolManager struct {
	mock.Mock
	pool.Manager
}

func (m *mockPoolManager) HasPool() bool {
	args := m.Called()
	return args.Bool(0)
}

func TestParseFile_EmptySegments(t *testing.T) {
	m := &mockPoolManager{}
	p := NewParser(m)

	nzbXML := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
 <file poster="poster" date="123456789" subject="test file">
  <groups>
   <group>alt.binaries.test</group>
  </groups>
  <segments>
  </segments>
 </file>
</nzb>`

	r := strings.NewReader(nzbXML)
	
	// We expect fetchAllFirstSegments to be called, which will return MissingFirstSegment for files with no segments.
	// Then it will fall back to fallbackGetFileInfos.
	m.On("HasPool").Return(false)

	parsed, err := p.ParseFile(context.Background(), r, "test.nzb")
	
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NZB file contains no valid files")
	assert.Nil(t, parsed)
}

func TestParseFile_MixedSegments(t *testing.T) {
	m := &mockPoolManager{}
	p := NewParser(m)

	nzbXML := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
 <file poster="poster" date="123456789" subject="file with segments">
  <groups>
   <group>alt.binaries.test</group>
  </groups>
  <segments>
   <segment bytes="100" number="1">seg1</segment>
  </segments>
 </file>
 <file poster="poster" date="123456789" subject="file without segments">
  <groups>
   <group>alt.binaries.test</group>
  </groups>
  <segments>
  </segments>
 </file>
</nzb>`

	r := strings.NewReader(nzbXML)
	
	// HasPool returns false to trigger fallback to fallbackGetFileInfos
	m.On("HasPool").Return(false)

	parsed, err := p.ParseFile(context.Background(), r, "test.nzb")
	
	assert.NoError(t, err)
	assert.NotNil(t, parsed)
	assert.Len(t, parsed.Files, 1)
	assert.Equal(t, "file with segments", parsed.Files[0].Filename)
}

func TestFallbackGetFileInfos_EmptySegments(t *testing.T) {
	p := NewParser(nil)
	
	files := []nzbparser.NzbFile{
		{
			Filename: "file1.txt",
			Segments: []nzbparser.NzbSegment{},
		},
		{
			Filename: "file2.txt",
			Segments: []nzbparser.NzbSegment{
				{ID: "seg1", Bytes: 100},
			},
		},
	}
	
	infos := p.fallbackGetFileInfos(files)
	
	assert.Len(t, infos, 1)
	assert.Equal(t, "file2.txt", infos[0].Filename)
}
