package sharenet_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/sharenet"
)

// setupStoreDir writes two .meta files under root and returns their virtual paths.
func setupStoreDir(t *testing.T) (root string, paths []string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "ShowName")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "episode.s01e01.meta"), []byte("meta-0"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "episode.s01e02.meta"), []byte("meta-1"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, []string{"ShowName/episode.s01e01", "ShowName/episode.s01e02"}
}

func TestStore_RegisterAndPaths(t *testing.T) {
	root, paths := setupStoreDir(t)
	s := sharenet.NewReleaseStore(root)

	s.Register("hash-abc", paths)
	got, ok := s.Paths("hash-abc")
	if !ok {
		t.Fatal("expected to find registered hash")
	}
	if len(got) != 2 || got[0] != paths[0] || got[1] != paths[1] {
		t.Fatalf("expected %v, got %v", paths, got)
	}
}

func TestStore_PathsMissing(t *testing.T) {
	s := sharenet.NewReleaseStore(t.TempDir())
	if _, ok := s.Paths("not-registered"); ok {
		t.Fatal("expected not found")
	}
}

func TestStore_ReadMetaByIndex(t *testing.T) {
	root, paths := setupStoreDir(t)
	s := sharenet.NewReleaseStore(root)
	s.Register("hash-abc", paths)

	for i, want := range []string{"meta-0", "meta-1"} {
		data, err := s.ReadMeta("hash-abc", i)
		if err != nil {
			t.Fatalf("index %d: unexpected error: %v", i, err)
		}
		if string(data) != want {
			t.Fatalf("index %d: expected %q, got %q", i, want, data)
		}
	}
}

func TestStore_ReadMetaIndexOutOfRange(t *testing.T) {
	root, paths := setupStoreDir(t)
	s := sharenet.NewReleaseStore(root)
	s.Register("hash-abc", paths)

	if _, err := s.ReadMeta("hash-abc", 2); err == nil {
		t.Fatal("expected error for out-of-range index")
	}
	if _, err := s.ReadMeta("hash-abc", -1); err == nil {
		t.Fatal("expected error for negative index")
	}
}

func TestStore_ReadMetaUnknownHash(t *testing.T) {
	s := sharenet.NewReleaseStore(t.TempDir())
	if _, err := s.ReadMeta("unknown", 0); err == nil {
		t.Fatal("expected error for unknown hash")
	}
}

// Register must copy its input so later mutation of the caller's slice does not
// corrupt the stored paths.
func TestStore_RegisterCopiesInput(t *testing.T) {
	root, paths := setupStoreDir(t)
	s := sharenet.NewReleaseStore(root)
	s.Register("hash-abc", paths)

	paths[0] = "mutated"
	got, _ := s.Paths("hash-abc")
	if got[0] == "mutated" {
		t.Fatal("Register must copy input slice")
	}
}
