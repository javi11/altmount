package par2repair

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestArenaAllocIsZeroedAndDiskBacked(t *testing.T) {
	dir := t.TempDir()
	a, err := newArena(dir, 8192)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()

	buf1, err := a.alloc(4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(buf1) != 4096 || !bytes.Equal(buf1, make([]byte, 4096)) {
		t.Fatal("alloc must return a zeroed buffer of the requested size")
	}
	copy(buf1, []byte("hello arena"))

	buf2, err := a.alloc(4096)
	if err != nil {
		t.Fatal(err)
	}
	copy(buf2, []byte("second"))
	if !bytes.HasPrefix(buf1, []byte("hello arena")) {
		t.Fatal("allocations must not overlap")
	}

	// The arena is a real file on disk, sized for its capacity.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want one backing file, got %d entries", len(entries))
	}
	info, err := os.Stat(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 8192 {
		t.Fatalf("backing file size = %d, want 8192", info.Size())
	}

	if _, err := a.alloc(1); err == nil {
		t.Fatal("alloc past capacity must fail")
	}
}

func TestArenaCloseRemovesBackingFile(t *testing.T) {
	dir := t.TempDir()
	a, err := newArena(dir, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.alloc(512); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("backing file must be removed on close, got %d entries", len(entries))
	}
}
