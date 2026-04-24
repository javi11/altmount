package nzbfile

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestIsGzippedAndPlainFilename(t *testing.T) {
	cases := []struct {
		in      string
		gzipped bool
		plainFn string
	}{
		{"movie.nzb", false, "movie.nzb"},
		{"movie.nzb.gz", true, "movie.nzb"},
		{"MOVIE.NZB.GZ", true, "MOVIE.NZB"},
		{"/a/b/movie.nzb.gz", true, "movie.nzb"},
	}
	for _, c := range cases {
		if got := IsGzipped(c.in); got != c.gzipped {
			t.Errorf("IsGzipped(%q)=%v want %v", c.in, got, c.gzipped)
		}
		if got := PlainFilename(c.in); got != c.plainFn {
			t.Errorf("PlainFilename(%q)=%q want %q", c.in, got, c.plainFn)
		}
	}
}

func TestOpenAndCompressRoundtrip(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "a.nzb")
	gz := filepath.Join(dir, "a.nzb.gz")
	content := []byte("<nzb>hello</nzb>")

	if err := os.WriteFile(plain, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Compress(plain, gz); err != nil {
		t.Fatalf("Compress: %v", err)
	}

	rc, err := Open(gz)
	if err != nil {
		t.Fatalf("Open gz: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("roundtrip mismatch: %q want %q", got, content)
	}

	rc2, err := Open(plain)
	if err != nil {
		t.Fatalf("Open plain: %v", err)
	}
	defer rc2.Close()
	got2, _ := io.ReadAll(rc2)
	if string(got2) != string(content) {
		t.Fatalf("plain read mismatch: %q", got2)
	}
}

func TestResolveOnDisk(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "only-plain.nzb")
	gz := filepath.Join(dir, "only-gz.nzb.gz")
	os.WriteFile(plain, []byte("x"), 0o644)
	os.WriteFile(gz, []byte("x"), 0o644)

	// Stored path matches disk.
	if got, err := ResolveOnDisk(plain); err != nil || got != plain {
		t.Errorf("plain exact: got=%q err=%v", got, err)
	}

	// Stored as .nzb, on disk as .nzb.gz.
	if got, err := ResolveOnDisk(filepath.Join(dir, "only-gz.nzb")); err != nil || got != gz {
		t.Errorf("drift .nzb→.nzb.gz: got=%q err=%v", got, err)
	}

	// Stored as .nzb.gz, on disk as .nzb.
	if got, err := ResolveOnDisk(filepath.Join(dir, "only-plain.nzb.gz")); err != nil || got != plain {
		t.Errorf("drift .nzb.gz→.nzb: got=%q err=%v", got, err)
	}

	// Neither exists.
	if _, err := ResolveOnDisk(filepath.Join(dir, "missing.nzb")); !os.IsNotExist(err) {
		t.Errorf("missing: err=%v", err)
	}
}
