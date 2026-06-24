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
