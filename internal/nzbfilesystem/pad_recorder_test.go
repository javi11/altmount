package nzbfilesystem

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/holes"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

type fakePadMetadataStore struct {
	mu             sync.Mutex
	knownHoles     []string
	statusUpdates  []string
	panicOnHoles   bool
	holesRecorded  chan struct{}
	statusRecorded chan struct{}
}

func (f *fakePadMetadataStore) AddKnownHoles(virtualPath string, runs []holes.Run) error {
	f.mu.Lock()
	panicNow := f.panicOnHoles
	f.panicOnHoles = false
	f.knownHoles = append(f.knownHoles, virtualPath)
	f.mu.Unlock()
	if f.holesRecorded != nil {
		f.holesRecorded <- struct{}{}
	}
	if panicNow {
		panic("boom")
	}
	return nil
}

func (f *fakePadMetadataStore) UpdateFileStatus(virtualPath string, status metapb.FileStatus) error {
	f.mu.Lock()
	f.statusUpdates = append(f.statusUpdates, virtualPath)
	f.mu.Unlock()
	if f.statusRecorded != nil {
		f.statusRecorded <- struct{}{}
	}
	return nil
}

type fakePadHealthStore struct {
	mu      sync.Mutex
	updates []string
}

func (f *fakePadHealthStore) UpdateFileHealthScheduled(ctx context.Context, filePath string, status database.HealthStatus, errorMessage *string, sourceNzbPath *string, errorDetails *string, noRetry bool, scheduledAt time.Time) error {
	f.mu.Lock()
	f.updates = append(f.updates, filePath)
	f.mu.Unlock()
	return nil
}

func (f *fakePadHealthStore) GetFileHealth(ctx context.Context, filePath string) (*database.FileHealth, error) {
	return nil, nil
}

// fakePadHealthStoreWithLibraryPath returns a FileHealth carrying a library
// path, exercising the EffectiveLibraryPath branch of triggerImmediateRepairSearch.
type fakePadHealthStoreWithLibraryPath struct {
	fakePadHealthStore
	libraryPath string
}

func (f *fakePadHealthStoreWithLibraryPath) GetFileHealth(ctx context.Context, filePath string) (*database.FileHealth, error) {
	lp := f.libraryPath
	return &database.FileHealth{FilePath: filePath, LibraryPath: &lp}, nil
}

// fakePadArrsTrigger records every TriggerFileRescan call it receives.
type fakePadArrsTrigger struct {
	mu       sync.Mutex
	calls    []string
	fired    chan struct{}
	returnFn func() error
}

func (f *fakePadArrsTrigger) TriggerFileRescan(ctx context.Context, pathForRescan string, relativePath string, metadataStr *string) error {
	f.mu.Lock()
	f.calls = append(f.calls, pathForRescan)
	f.mu.Unlock()
	if f.fired != nil {
		f.fired <- struct{}{}
	}
	if f.returnFn != nil {
		return f.returnFn()
	}
	return nil
}

func testConfigGetter() config.ConfigGetter {
	return func() *config.Config {
		return &config.Config{MountPath: "/mnt/bearmount"}
	}
}

func testPadEvent(name string) padEvent {
	return padEvent{
		name:          name,
		segIndex:      3,
		sourceNzbPath: "/nzb/" + name + ".nzb",
		fileSize:      100 << 20,
		total:         1,
		longest:       1,
		totalSegments: 200,
		segBytes:      512 << 10,
	}
}

func TestPadRecorderPersistsEvent(t *testing.T) {
	meta := &fakePadMetadataStore{holesRecorded: make(chan struct{}, 1)}
	health := &fakePadHealthStore{}
	r := newPadRecorder(meta, health, nil, nil, nil)

	r.enqueue(testPadEvent("movie.mkv"))

	select {
	case <-meta.holesRecorded:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for hole persistence")
	}
	r.Close() // drains, so the status/health writes are done after this

	meta.mu.Lock()
	defer meta.mu.Unlock()
	if len(meta.knownHoles) != 1 || meta.knownHoles[0] != "movie.mkv" {
		t.Fatalf("knownHoles = %v, want [movie.mkv]", meta.knownHoles)
	}
	if len(meta.statusUpdates) != 1 {
		t.Fatalf("statusUpdates = %v, want one entry", meta.statusUpdates)
	}
	health.mu.Lock()
	defer health.mu.Unlock()
	if len(health.updates) != 1 || health.updates[0] != "movie.mkv" {
		t.Fatalf("health updates = %v, want [movie.mkv]", health.updates)
	}
}

func TestPadRecorderTriggersImmediateRepairSearch(t *testing.T) {
	meta := &fakePadMetadataStore{holesRecorded: make(chan struct{}, 1)}
	health := &fakePadHealthStoreWithLibraryPath{libraryPath: "/data/movies/Movie (2020)/Movie.mkv"}
	arrs := &fakePadArrsTrigger{fired: make(chan struct{}, 1)}
	r := newPadRecorder(meta, health, nil, arrs, testConfigGetter())

	r.enqueue(testPadEvent("movie.mkv"))

	select {
	case <-arrs.fired:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for immediate repair-search trigger")
	}
	r.Close()

	arrs.mu.Lock()
	defer arrs.mu.Unlock()
	if len(arrs.calls) != 1 || arrs.calls[0] != "/data/movies/Movie (2020)/Movie.mkv" {
		t.Fatalf("TriggerFileRescan calls = %v, want one call for the library path", arrs.calls)
	}
}

// TestPadRecorderRepairSearchFallsBackToMountPath covers a file with no
// library path yet (not relinked by an ARR) - triggerImmediateRepairSearch
// must still fire, resolving the fallback path the same way
// health.HealthWorker.resolvePathForRescan does (MountPath + virtual path).
func TestPadRecorderRepairSearchFallsBackToMountPath(t *testing.T) {
	meta := &fakePadMetadataStore{holesRecorded: make(chan struct{}, 1)}
	health := &fakePadHealthStore{} // GetFileHealth returns (nil, nil): no library path
	arrs := &fakePadArrsTrigger{fired: make(chan struct{}, 1)}
	r := newPadRecorder(meta, health, nil, arrs, testConfigGetter())

	r.enqueue(testPadEvent("movie.mkv"))

	select {
	case <-arrs.fired:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for immediate repair-search trigger")
	}
	r.Close()

	arrs.mu.Lock()
	defer arrs.mu.Unlock()
	if len(arrs.calls) != 1 || arrs.calls[0] != "/mnt/bearmount/movie.mkv" {
		t.Fatalf("TriggerFileRescan calls = %v, want one call for the mount-path fallback", arrs.calls)
	}
}

// TestPadRecorderRepairSearchNilArrsIsNoop covers the padArrsTrigger==nil
// tolerance (test harnesses / any deployment where health monitoring is off
// but padding still applies): must not panic, must not block enqueue.
func TestPadRecorderRepairSearchNilArrsIsNoop(t *testing.T) {
	meta := &fakePadMetadataStore{holesRecorded: make(chan struct{}, 1)}
	health := &fakePadHealthStore{}
	r := newPadRecorder(meta, health, nil, nil, nil)

	r.enqueue(testPadEvent("movie.mkv"))

	select {
	case <-meta.holesRecorded:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for hole persistence")
	}
	r.Close()
}

func TestPadRecorderSurvivesPanic(t *testing.T) {
	meta := &fakePadMetadataStore{
		panicOnHoles:  true,
		holesRecorded: make(chan struct{}, 2),
	}
	r := newPadRecorder(meta, &fakePadHealthStore{}, nil, nil, nil)

	r.enqueue(testPadEvent("first.mkv")) // panics after recording the hole
	r.enqueue(testPadEvent("second.mkv"))

	for i := 0; i < 2; i++ {
		select {
		case <-meta.holesRecorded:
		case <-time.After(5 * time.Second):
			t.Fatalf("worker died: only %d of 2 events processed", i)
		}
	}
	r.Close()
}

func TestPadRecorderEnqueueDropsWhenFull(t *testing.T) {
	// No worker: construct the struct directly so nothing drains the buffer.
	r := &padRecorder{ch: make(chan padEvent, 1)}
	r.enqueue(testPadEvent("a.mkv"))
	r.enqueue(testPadEvent("b.mkv")) // must not block
	if got := len(r.ch); got != 1 {
		t.Fatalf("buffered events = %d, want 1", got)
	}
}

func TestPadRecorderNilEnqueueIsNoop(t *testing.T) {
	var r *padRecorder
	r.enqueue(testPadEvent("a.mkv")) // must not panic
}

// TestOnHoleDoesNotRaceClose reproduces the PR #774 crash scenario: a hole
// verdict on a detached download goroutine while Close() nils mvf.meta.
// Run with -race; the snapshot in holeHooks() must keep onHole off mvf.meta.
func TestOnHoleDoesNotRaceClose(t *testing.T) {
	mvf := &MetadataVirtualFile{
		name: "movie.mkv",
		ctx:  context.Background(),
		meta: &fileHandleMeta{
			FileSize:    100 << 20,
			SegmentData: make([]*metapb.SegmentData, 200),
		},
	}
	if mvf.holeHooks() == nil {
		t.Fatal("expected hole hooks for eligible file")
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if d := mvf.onHole(7, "seg-7"); d != holes.DecisionPad {
			t.Errorf("onHole decision = %v, want pad", d)
		}
	}()
	go func() {
		defer wg.Done()
		_ = mvf.Close()
	}()
	wg.Wait()
}
