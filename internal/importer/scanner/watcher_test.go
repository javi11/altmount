package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
)

// stubQueueAdder captures what processNzb passes to AddToQueue so the test
// can assert relativePath has no OS-native backslashes (regression for
// issue #585 Bug B on Windows).
type stubQueueAdder struct {
	mu                 sync.Mutex
	lastRelativePath   *string
	lastCategory       *string
	addToQueueCalls    int
	isFileInQueueCalls int
}

func (s *stubQueueAdder) AddToQueue(
	_ context.Context,
	_ string,
	relativePath *string,
	category *string,
	_ *database.QueuePriority,
	_ *string,
	_ *string,
) (*database.ImportQueueItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addToQueueCalls++
	s.lastRelativePath = relativePath
	s.lastCategory = category
	return &database.ImportQueueItem{ID: 1}, nil
}

func (s *stubQueueAdder) IsFileInQueue(_ context.Context, _ string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isFileInQueueCalls++
	return false, nil
}

// TestProcessNzb_RelativePathHasNoBackslashes is a regression test for #585
// Bug B. The watcher must store relativePath using forward slashes so
// downstream virtual-path computation cannot accidentally join an absolute
// Windows-shaped value into a filesystem path like
// filepath.Join("D:\Metadata", "D:\Downloads\…").
//
// On Linux this passes trivially because filepath.Rel already returns
// forward slashes. On Windows it would fail without the
// `relPath = filepath.ToSlash(relPath)` normalisation in watcher.processNzb.
func TestProcessNzb_RelativePathHasNoBackslashes(t *testing.T) {
	tmp := t.TempDir()
	watchRoot := filepath.Join(tmp, "watch")
	subDir := filepath.Join(watchRoot, "sub", "nested")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	nzbPath := filepath.Join(subDir, "release.nzb")
	if err := os.WriteFile(nzbPath, []byte("<nzb/>"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg := &config.Config{}
	stub := &stubQueueAdder{}
	w := NewWatcher(stub, func() *config.Config { return cfg })

	if err := w.processNzb(context.Background(), watchRoot, nzbPath); err != nil {
		t.Fatalf("processNzb: %v", err)
	}

	if stub.addToQueueCalls != 1 {
		t.Fatalf("AddToQueue calls = %d, want 1", stub.addToQueueCalls)
	}
	if stub.lastRelativePath == nil {
		t.Fatalf("relativePath was nil, want %q", "sub/nested")
	}
	got := *stub.lastRelativePath
	if strings.ContainsRune(got, '\\') {
		t.Errorf("relativePath = %q contains backslash; watcher must normalise to forward slashes", got)
	}
	if got != "sub/nested" {
		t.Errorf("relativePath = %q, want %q", got, "sub/nested")
	}
}
