package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveEmptyDirs(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "altmount-test-remove-dirs")
	err := os.MkdirAll(tempDir, 0755)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	root := filepath.Join(tempDir, "root")
	err = os.MkdirAll(root, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create nested empty directories: root/a/b/c
	nested := filepath.Join(root, "a", "b", "c")
	err = os.MkdirAll(nested, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Remove c, and expect b and a to be removed too
	RemoveEmptyDirs(root, nested)

	// Check if a, b, c were removed
	for _, dir := range []string{"a", "a/b", "a/b/c"} {
		path := filepath.Join(root, dir)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("Expected directory %s to be removed, but it exists", path)
		}
	}

	// Check if root still exists
	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Error("Expected root directory to exist, but it was removed")
	}

	// Test with non-empty directory
	// root/x/y/z, with root/x/keep.txt
	xDir := filepath.Join(root, "x")
	yDir := filepath.Join(xDir, "y")
	zDir := filepath.Join(yDir, "z")
	err = os.MkdirAll(zDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	keepFile := filepath.Join(xDir, "keep.txt")
	err = os.WriteFile(keepFile, []byte("keep"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Remove z, and expect y to be removed, but x should stay
	RemoveEmptyDirs(root, zDir)

	if _, err := os.Stat(zDir); err == nil {
		t.Error("Expected zDir to be removed")
	}
	if _, err := os.Stat(yDir); err == nil {
		t.Error("Expected yDir to be removed")
	}
	if _, err := os.Stat(xDir); os.IsNotExist(err) {
		t.Error("Expected xDir to still exist because it contains keep.txt")
	}
	if _, err := os.Stat(keepFile); os.IsNotExist(err) {
		t.Error("Expected keep.txt to still exist")
	}
}
