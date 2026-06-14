package filesystem

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
)

func TestUsenetFileSystemResolvesWidthMismatchVolume(t *testing.T) {
	base := "Meet.Joe.Black.1998.REMUX"
	files := []parser.ParsedFile{
		{Filename: base + ".part09.rar", Size: 100},
		{Filename: base + ".part010.rar", Size: 200},
	}
	ufs := NewUsenetFileSystem(context.Background(), nil, files, 1, nil, 0)

	// rardecode computes "…part10.rar" (fixed 2-digit width) for the volume after
	// part09; it must resolve to the real "…part010.rar".
	f, err := ufs.Open(base + ".part10.rar")
	if err != nil {
		t.Fatalf("Open(part10.rar) returned error: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	info, err := ufs.Stat(base + ".part10.rar")
	if err != nil {
		t.Fatalf("Stat(part10.rar) returned error: %v", err)
	}
	if info.Size() != 200 {
		t.Errorf("Stat size = %d; want 200 (the part010 entry)", info.Size())
	}

	// A genuinely-absent volume must still report not-exist.
	if _, err := ufs.Open(base + ".part99.rar"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open(absent) err = %v; want fs.ErrNotExist", err)
	}
}
