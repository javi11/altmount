package sharenet_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/sharenet"
)

func setupStoreDir(t *testing.T) (root, virtualPath string) {
	t.Helper()
	root = t.TempDir()
	virtualPath = "ShowName/episode.s01e01"
	dir := filepath.Join(root, "ShowName")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "episode.s01e01.meta"), []byte("meta-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "episode.s01e01.seg"), []byte("seg-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, virtualPath
}

func TestStore_RegisterAndLookup(t *testing.T) {
	root, virtualPath := setupStoreDir(t)
	s := sharenet.NewReleaseStore(root)

	s.Register("hash-abc", virtualPath)
	got, ok := s.Lookup("hash-abc")
	if !ok {
		t.Fatal("expected to find registered hash")
	}
	if got != virtualPath {
		t.Fatalf("expected %q, got %q", virtualPath, got)
	}
}

func TestStore_LookupMissing(t *testing.T) {
	s := sharenet.NewReleaseStore(t.TempDir())
	_, ok := s.Lookup("not-registered")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestStore_ReadMeta(t *testing.T) {
	root, virtualPath := setupStoreDir(t)
	s := sharenet.NewReleaseStore(root)
	s.Register("hash-abc", virtualPath)

	data, err := s.ReadMeta("hash-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "meta-bytes" {
		t.Fatalf("expected meta-bytes, got %q", data)
	}
}

func TestStore_ReadSeg(t *testing.T) {
	root, virtualPath := setupStoreDir(t)
	s := sharenet.NewReleaseStore(root)
	s.Register("hash-abc", virtualPath)

	data, err := s.ReadSeg("hash-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "seg-bytes" {
		t.Fatalf("expected seg-bytes, got %q", data)
	}
}

func TestStore_ReadMeta_UnknownHash(t *testing.T) {
	s := sharenet.NewReleaseStore(t.TempDir())
	_, err := s.ReadMeta("unknown")
	if err == nil {
		t.Fatal("expected error for unknown hash")
	}
}
