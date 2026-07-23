package parser

import (
	"testing"

	"github.com/javi11/nzbparser"
)

func TestSanitizeNzbFilenames(t *testing.T) {
	n := &nzbparser.Nzb{
		Files: nzbparser.NzbFiles{
			{Filename: "'movie.part001.rar'", Subject: "[1/5] - 'movie.part001.rar' yEnc (1/50)"},
			{Filename: `"X.part01.rar"`},
			{Filename: "It's.A.Wonderful.Life.part01.rar"},
			{Filename: "clean.mkv"},
		},
	}

	SanitizeNzbFilenames(n)

	want := []string{
		"movie.part001.rar",
		"X.part01.rar",
		"It's.A.Wonderful.Life.part01.rar",
		"clean.mkv",
	}
	for i, w := range want {
		if got := n.Files[i].Filename; got != w {
			t.Errorf("Files[%d].Filename = %q; want %q", i, got, w)
		}
	}

	if got := n.Files[0].Subject; got != "[1/5] - 'movie.part001.rar' yEnc (1/50)" {
		t.Errorf("raw Subject must be preserved, got %q", got)
	}
}

func TestSanitizeNzbFilenamesNil(t *testing.T) {
	SanitizeNzbFilenames(nil)
}

// TestSanitizeNzbFilenames_Traversal covers a real fix: a filename pulled
// straight out of an NZB file entry is poster-controlled content (any
// indexer an *arr app auto-grabs from can serve one with no human review),
// and downstream consumers build real filesystem/metadata paths from it via
// filepath.Join, which does not reject a ".." segment on its own. A
// traversal attempt must collapse to its base filename rather than surviving
// into the sanitized output.
func TestSanitizeNzbFilenames_Traversal(t *testing.T) {
	n := &nzbparser.Nzb{
		Files: nzbparser.NzbFiles{
			{Filename: "../../../etc/passwd"},
			{Filename: `..\..\windows\evil.mkv`},
			{Filename: "'../../etc/passwd'"}, // quoted traversal
			{Filename: "movies/../../etc/passwd"},
			{Filename: "safe/subdir/movie.mkv"}, // legitimate nesting untouched
			{Filename: "movie..2024.mkv"},        // traversal-looking but safe
		},
	}

	SanitizeNzbFilenames(n)

	want := []string{
		"passwd",
		"evil.mkv",
		"passwd",
		"passwd",
		"safe/subdir/movie.mkv",
		"movie..2024.mkv",
	}
	for i, w := range want {
		if got := n.Files[i].Filename; got != w {
			t.Errorf("Files[%d].Filename = %q; want %q", i, got, w)
		}
	}
}
