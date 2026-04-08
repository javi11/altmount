package filesystem

import (
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
)

func TestSeparateFiles_7zSplitArchive(t *testing.T) {
	// Simulate a 68-part split 7z archive where only the first part has Is7zArchive=true
	// (because only .7z.001 contains the 7z magic bytes header).
	files := make([]parser.ParsedFile, 0, 70)

	// First archive part — has magic bytes detected
	files = append(files, parser.ParsedFile{
		Filename:    "3461640925132534.7z.001",
		Is7zArchive: true,
	})

	// Remaining archive parts — no magic bytes, Is7zArchive=false
	for i := 2; i <= 68; i++ {
		files = append(files, parser.ParsedFile{
			Filename:    "3461640925132534.7z.001",
			Is7zArchive: false,
		})
	}

	// Par2 repair files
	files = append(files, parser.ParsedFile{
		Filename:      "3461640925132534.7z.par2",
		IsPar2Archive: true,
	})
	files = append(files, parser.ParsedFile{
		Filename:      "3461640925132534.7z.vol00+01.par2",
		IsPar2Archive: true,
	})

	regular, archive, par2 := SeparateFiles(files, parser.NzbType7zArchive)

	if len(regular) != 0 {
		t.Errorf("expected 0 regular files, got %d", len(regular))
	}
	if len(archive) != 68 {
		t.Errorf("expected 68 archive files, got %d", len(archive))
	}
	if len(par2) != 2 {
		t.Errorf("expected 2 par2 files, got %d", len(par2))
	}
}

func TestSeparateFiles_7zPar2NotMisclassified(t *testing.T) {
	// A .7z.par2 file that also has Is7zArchive=true should still land in par2, not archive.
	files := []parser.ParsedFile{
		{Filename: "movie.7z.001", Is7zArchive: true},
		{Filename: "movie.7z.par2", Is7zArchive: true, IsPar2Archive: true},
	}

	_, archive, par2 := SeparateFiles(files, parser.NzbType7zArchive)

	if len(archive) != 1 {
		t.Errorf("expected 1 archive file, got %d", len(archive))
	}
	if len(par2) != 1 {
		t.Errorf("expected 1 par2 file, got %d", len(par2))
	}
	if par2[0].Filename != "movie.7z.par2" {
		t.Errorf("expected par2 file to be movie.7z.par2, got %s", par2[0].Filename)
	}
}
