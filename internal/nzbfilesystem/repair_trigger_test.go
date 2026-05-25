package nzbfilesystem

import (
	"context"
	"sync"
	"testing"

	"github.com/javi11/altmount/internal/usenet"
)

type fakeRepair struct {
	mu     sync.Mutex
	called bool
	path   string
	err    error
}

func (f *fakeRepair) RepairFile(_ context.Context, p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.path = p
	return f.err
}

// TestSelfHealSuccessSkipsFallback verifies that when the repair service heals
// the file, selfHealOrFallback does NOT fall through to the mark-corrupted/ARR
// path. The mvf is built with nil metadata/health dependencies on purpose: if
// the fallback were reached it would dereference them and panic, so a clean run
// proves the success path was taken.
func TestSelfHealSuccessSkipsFallback(t *testing.T) {
	fr := &fakeRepair{}
	mvf := &MetadataVirtualFile{
		name:            "/movies/x.mkv",
		repairService:   fr,
		repairCoalescer: nil, // EnqueueRefresh is nil-safe
	}

	mvf.selfHealOrFallback(&usenet.DataCorruptionError{}, true)

	if !fr.called {
		t.Fatal("repair service was not invoked")
	}
	if fr.path != "/movies/x.mkv" {
		t.Fatalf("repair invoked with path %q, want /movies/x.mkv", fr.path)
	}
}
