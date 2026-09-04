package par2repair

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestPatchStoreRoundTrip(t *testing.T) {
	p := NewPatchStore(t.TempDir())
	payload := bytes.Repeat([]byte{0xCD}, 1500)

	if _, ok := p.Get("<a@test>"); ok {
		t.Fatal("unexpected hit before Put")
	}
	if p.Has("<a@test>") {
		t.Fatal("unexpected Has before Put")
	}
	if err := p.Put("<a@test>", payload); err != nil {
		t.Fatal(err)
	}
	got, ok := p.Get("<a@test>")
	if !ok || !bytes.Equal(got, payload) {
		t.Fatal("payload mismatch")
	}
	if !p.Has("<a@test>") {
		t.Fatal("Has = false after Put")
	}
	// Distinct key stays a miss.
	if _, ok := p.Get("<b@test>"); ok {
		t.Fatal("unexpected hit for other key")
	}
}

func TestPatchStoreEmptyMessageID(t *testing.T) {
	p := NewPatchStore(t.TempDir())
	if err := p.Put("", []byte("x")); err == nil {
		t.Fatal("want error for empty message ID")
	}
}

func TestPatchStorePersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	if err := NewPatchStore(dir).Put("<a@test>", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, ok := NewPatchStore(dir).Get("<a@test>")
	if !ok || string(got) != "hello" {
		t.Fatal("patch not visible to a fresh store over the same dir")
	}
}

// putPatchAt writes a patch and pins its mtime so eviction order is
// deterministic.
func putPatchAt(t *testing.T, p *PatchStore, id string, size int, mtime time.Time) {
	t.Helper()
	if err := p.Put(id, bytes.Repeat([]byte{0xAB}, size)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p.path(id), mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestPatchStorePruneEvictsOldestFirst(t *testing.T) {
	p := NewPatchStore(t.TempDir())
	now := time.Now()
	putPatchAt(t, p, "<old@test>", 1000, now.Add(-3*time.Hour))
	putPatchAt(t, p, "<mid@test>", 1000, now.Add(-2*time.Hour))
	putPatchAt(t, p, "<new@test>", 1000, now.Add(-1*time.Hour))

	// Cap of 2500 bytes: total 3000 exceeds it, evicting only the oldest
	// brings it to 2000.
	if err := p.Prune(2500); err != nil {
		t.Fatal(err)
	}
	if p.Has("<old@test>") {
		t.Fatal("oldest patch must be evicted")
	}
	if !p.Has("<mid@test>") || !p.Has("<new@test>") {
		t.Fatal("newer patches must survive")
	}

	// Cap of 1500: evicts mid, keeps new.
	if err := p.Prune(1500); err != nil {
		t.Fatal(err)
	}
	if p.Has("<mid@test>") {
		t.Fatal("mid patch must be evicted at the tighter cap")
	}
	if !p.Has("<new@test>") {
		t.Fatal("newest patch must survive")
	}
}

func TestPatchStorePruneNoOpCases(t *testing.T) {
	p := NewPatchStore(t.TempDir())
	now := time.Now()
	putPatchAt(t, p, "<a@test>", 1000, now.Add(-time.Hour))
	putPatchAt(t, p, "<b@test>", 1000, now)

	// Zero and negative caps are no-ops.
	if err := p.Prune(0); err != nil {
		t.Fatal(err)
	}
	if err := p.Prune(-1); err != nil {
		t.Fatal(err)
	}
	// Under the cap: nothing evicted.
	if err := p.Prune(10_000); err != nil {
		t.Fatal(err)
	}
	if !p.Has("<a@test>") || !p.Has("<b@test>") {
		t.Fatal("prune must not evict when under the cap or with cap <= 0")
	}
}

func TestPatchStorePruneMissingRoot(t *testing.T) {
	// A store that never stored anything has no root directory yet.
	p := NewPatchStore(t.TempDir() + "/nonexistent")
	if err := p.Prune(100); err != nil {
		t.Fatalf("prune over missing root must be a no-op, got %v", err)
	}
}

// A concurrent reader must always see a complete payload (one of the two
// alternating writes) or a miss — never a partial file.
func TestPatchStoreAtomicPut(t *testing.T) {
	p := NewPatchStore(t.TempDir())
	small := bytes.Repeat([]byte{0x01}, 100)
	large := bytes.Repeat([]byte{0x02}, 100_000)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			got, ok := p.Get("<hot@test>")
			if !ok {
				continue
			}
			if !bytes.Equal(got, small) && !bytes.Equal(got, large) {
				t.Errorf("partial payload observed: %d bytes", len(got))
				return
			}
		}
	}()

	for i := range 100 {
		payload := small
		if i%2 == 1 {
			payload = large
		}
		if err := p.Put("<hot@test>", payload); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
}

func TestPatchStoreRemoveScratch(t *testing.T) {
	store := NewPatchStore(t.TempDir())
	if err := store.Put("<keep@x>", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	scratch := store.ScratchDir()
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, ".par2repair-1.mem"), []byte("arena"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.RemoveScratch(); err != nil {
		t.Fatalf("RemoveScratch: %v", err)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("scratch dir still present, stat err = %v", err)
	}
	if _, ok := store.Get("<keep@x>"); !ok {
		t.Fatal("RemoveScratch deleted a stored patch")
	}
	// Idempotent: a second call on a missing dir is not an error.
	if err := store.RemoveScratch(); err != nil {
		t.Fatalf("second RemoveScratch: %v", err)
	}
}

func TestPatchStoreSweepTempFiles(t *testing.T) {
	root := t.TempDir()
	store := NewPatchStore(root)
	if err := store.Put("<keep@x>", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	// A leftover temp file in the same fanned-out dir as the real patch.
	dir := filepath.Join(root, "ab")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, ".tmp-orphan")
	if err := os.WriteFile(tmp, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.SweepTempFiles(); err != nil {
		t.Fatalf("SweepTempFiles: %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("temp file survived, stat err = %v", err)
	}
	if _, ok := store.Get("<keep@x>"); !ok {
		t.Fatal("SweepTempFiles deleted a stored patch")
	}
}

func TestPatchStoreSweepersOnMissingRoot(t *testing.T) {
	store := NewPatchStore(filepath.Join(t.TempDir(), "never-created"))
	if err := store.RemoveScratch(); err != nil {
		t.Fatalf("RemoveScratch on missing root: %v", err)
	}
	if err := store.SweepTempFiles(); err != nil {
		t.Fatalf("SweepTempFiles on missing root: %v", err)
	}
}
