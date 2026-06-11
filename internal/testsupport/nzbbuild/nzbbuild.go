// Package nzbbuild provides helpers for constructing NZB objects and writing
// them to temporary files in tests.
package nzbbuild

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/nzbparser"
)

// Segment describes one <segment> entry in an NZB file.
type Segment struct {
	// ID is the NNTP message-ID (e.g. "test-seg-001@fake").
	ID string
	// Bytes is the declared segment size as it would appear in the NZB
	// <segment bytes="N"> attribute.  Set this higher than the real decoded
	// payload to simulate yEnc/article overhead.
	Bytes int
}

// File describes one <file> entry in an NZB.
type File struct {
	// Subject is used as both the XML subject attribute and the parsed filename.
	Subject string
	// Groups defaults to ["alt.binaries.test"] when nil.
	Groups   []string
	Segments []Segment
	// Date is the Unix timestamp for the file entry (0 is fine for tests).
	Date int
}

// Build assembles a *nzbparser.Nzb from the provided file descriptions.
// The resulting object can be passed to nzbparser.Write for disk serialisation
// or used directly with any in-process helper that accepts *nzbparser.Nzb.
func Build(files ...File) *nzbparser.Nzb {
	defaultGroups := []string{"alt.binaries.test"}
	nzbFiles := make(nzbparser.NzbFiles, len(files))
	for i, f := range files {
		groups := f.Groups
		if len(groups) == 0 {
			groups = defaultGroups
		}
		segs := make(nzbparser.NzbSegments, len(f.Segments))
		for j, s := range f.Segments {
			segs[j] = nzbparser.NzbSegment{
				Bytes:  s.Bytes,
				Number: j + 1,
				ID:     s.ID,
			}
		}
		nzbFiles[i] = nzbparser.NzbFile{
			Subject:  f.Subject,
			Filename: f.Subject,
			Groups:   groups,
			Segments: segs,
			Date:     f.Date,
		}
	}
	return &nzbparser.Nzb{Files: nzbFiles}
}

// WriteTemp renders n to a .nzb file inside t.TempDir() and returns its path.
// name should be the base filename without extension (e.g. "My.Release.2024").
func WriteTemp(t *testing.T, n *nzbparser.Nzb, name string) string {
	t.Helper()
	data, err := nzbparser.Write(n)
	if err != nil {
		t.Fatalf("nzbbuild.WriteTemp: nzbparser.Write failed: %v", err)
	}
	path := filepath.Join(t.TempDir(), name+".nzb")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("nzbbuild.WriteTemp: WriteFile failed: %v", err)
	}
	return path
}
