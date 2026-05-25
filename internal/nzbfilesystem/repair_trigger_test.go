package nzbfilesystem

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
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

func (f *fakeRepair) ProbeRepairable(_ context.Context, _ string) (bool, bool, error) {
	return false, false, nil
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

	mvf.selfHealOrFallback(&usenet.DataCorruptionError{}, true, fileFailureSnapshot{})

	if !fr.called {
		t.Fatal("repair service was not invoked")
	}
	if fr.path != "/movies/x.mkv" {
		t.Fatalf("repair invoked with path %q, want /movies/x.mkv", fr.path)
	}
}

// TestMarkCorruptedUsesSnapshotNotMeta is the regression guard for the
// background-goroutine race: markCorruptedAndTriggerArr must operate off the
// captured snapshot, not mvf.meta, which Close() nils. With mvf.meta == nil it
// must neither panic nor lose the source-NZB path.
func TestMarkCorruptedUsesSnapshotNotMeta(t *testing.T) {
	dir := t.TempDir()
	db, err := database.NewDB(database.Config{DatabasePath: filepath.Join(dir, "health.db")})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	hr := database.NewHealthRepository(db.Connection(), database.DialectSQLite)

	maskingOff := false
	cfg := &config.Config{MountPath: "/mnt"}
	cfg.Streaming.FailureMasking.Enabled = &maskingOff

	mvf := &MetadataVirtualFile{
		name:             "/movies/x.mkv",
		meta:             nil, // simulate the handle having been Close()d
		metadataService:  metadata.NewMetadataService(dir),
		healthRepository: hr,
		repairCoalescer:  nil, // EnqueueRefresh is nil-safe
		configGetter:     func() *config.Config { return cfg },
	}

	snap := fileFailureSnapshot{sourceNzbPath: "/nzbs/x.nzb", segmentCount: 42}

	// Must not panic even though mvf.meta is nil.
	corruptErr := &usenet.DataCorruptionError{UnderlyingErr: errors.New("missing article")}
	mvf.markCorruptedAndTriggerArr(context.Background(), corruptErr, true, snap)

	// The snapshot's source path must have been persisted (proving the fallback
	// read it from the snapshot, not from the nil meta).
	health, err := hr.GetFileHealth(context.Background(), "/movies/x.mkv")
	if err != nil {
		t.Fatalf("GetFileHealth: %v", err)
	}
	if health == nil {
		t.Fatal("expected a health record to be written")
	}
	if health.SourceNzbPath == nil || *health.SourceNzbPath != "/nzbs/x.nzb" {
		t.Fatalf("source_nzb_path = %v, want /nzbs/x.nzb (from snapshot)", health.SourceNzbPath)
	}
}
