//go:build !windows

package utils

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPinSymlinkTimeDoesNotModifyTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	targetTime := time.Date(2020, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(target, targetTime, targetTime); err != nil {
		t.Fatal(err)
	}
	pinned := time.Date(2024, time.June, 7, 8, 9, 10, 0, time.UTC)
	if err := PinSymlinkTime(link, pinned.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	targetInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !targetInfo.ModTime().Equal(targetTime) {
		t.Fatalf("target mtime changed: got %v, want %v", targetInfo.ModTime(), targetTime)
	}
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if !linkInfo.ModTime().Equal(pinned) {
		t.Fatalf("symlink mtime = %v, want %v", linkInfo.ModTime(), pinned)
	}
}
