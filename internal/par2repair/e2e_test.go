package par2repair

import (
	"bytes"
	"context"
	"testing"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

type fakeMetaSource struct {
	fm    *metapb.FileMetadata
	store *metapb.NzbStore
}

func (f *fakeMetaSource) ReadFileMetadata(string) (*metapb.FileMetadata, error) { return f.fm, nil }
func (f *fakeMetaSource) ReadStore(string) (*metapb.NzbStore, error)           { return f.store, nil }

// End-to-end through the service: enqueue -> claim -> resolve -> sweep ->
// solve -> patch persisted -> job terminal -> patch survives a "restart".
func TestServiceEndToEndRepair(t *testing.T) {
	fm, store, fetch, contents, deadID := mkResolveFixture(t, false)
	fm.StoreRef = "store-ref" // route the service through ReadStore
	// Persist the hole the way playback would: KnownHoles + SegmentData.
	fm.SegmentData = []*metapb.SegmentData{
		{Id: "a.rar-0@test"}, {Id: deadID}, {Id: "a.rar-2@test"}, {Id: "a.rar-3@test"},
	}
	fm.KnownHoles = []*metapb.HoleRun{{StartSegment: 1, Count: 1}}

	repo := newTestRepo(t)
	patchDir := t.TempDir()
	cfg := func() Config {
		return Config{Enabled: true, MaxRepairRatio: 0.5, MaxMemoryMB: 256, MaxConcurrentJobs: 1}
	}
	s := NewService(repo, &fakeMetaSource{fm: fm, store: store}, fetch, NewPatchStore(patchDir), cfg, testLogger())

	// Trigger without a failing segment: known holes alone must be enough.
	s.Enqueue(context.Background(), "/movies/a.mkv", "")
	if !s.runNext(context.Background()) {
		t.Fatal("no job claimed")
	}

	// Patch stored and byte-exact.
	got, ok := s.PatchStore().Get(deadID)
	if !ok || !bytes.Equal(got, contents["a.rar"][2048:4096]) {
		t.Fatal("patch missing or not byte-exact after service repair")
	}

	// Job is terminal (repaired): nothing claimable.
	job, err := repo.ClaimNext(context.Background(), time.Now().UTC().Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if job != nil {
		t.Fatalf("job still claimable after success: %+v", job)
	}

	// "Restart": a fresh PatchStore over the same directory still serves it.
	got, ok = NewPatchStore(patchDir).Get(deadID)
	if !ok || !bytes.Equal(got, contents["a.rar"][2048:4096]) {
		t.Fatal("patch did not survive restart")
	}
}

// A release without enough recovery data ends terminal-unrepairable, never
// retried forever.
func TestServiceEndToEndUnrepairable(t *testing.T) {
	fm, store, fetch, _, deadID := mkResolveFixture(t, false)
	fm.StoreRef = "store-ref"
	// Remove every par2 volume from metadata: index only, zero recovery slices.
	fm.Par2Files = fm.Par2Files[:1]

	repo := newTestRepo(t)
	cfg := func() Config {
		return Config{Enabled: true, MaxRepairRatio: 0.5, MaxMemoryMB: 256, MaxConcurrentJobs: 1}
	}
	s := NewService(repo, &fakeMetaSource{fm: fm, store: store}, fetch, NewPatchStore(t.TempDir()), cfg, testLogger())

	s.Enqueue(context.Background(), "/movies/a.mkv", deadID)
	if !s.runNext(context.Background()) {
		t.Fatal("no job claimed")
	}
	job, err := repo.ClaimNext(context.Background(), time.Now().UTC().Add(1000*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if job != nil {
		t.Fatalf("unrepairable job still claimable: %+v", job)
	}
}
