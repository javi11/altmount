package nzbfilesystem

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeRepairEnqueuer struct {
	mu    sync.Mutex
	calls []struct{ path, segID string }
}

func (f *fakeRepairEnqueuer) Enqueue(_ context.Context, filePath, failingSegmentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct{ path, segID string }{filePath, failingSegmentID})
}

// A recorded pad event queues a PAR2 repair carrying the failing segment ID.
func TestPadRecorderEnqueuesRepair(t *testing.T) {
	meta := &fakePadMetadataStore{holesRecorded: make(chan struct{}, 1)}
	repair := &fakeRepairEnqueuer{}
	r := newPadRecorder(meta, &fakePadHealthStore{}, nil)
	r.repair = repair

	ev := testPadEvent("movie.mkv")
	ev.segID = "<dead@test>"
	r.enqueue(ev)

	select {
	case <-meta.holesRecorded:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for hole persistence")
	}
	r.Close()

	repair.mu.Lock()
	defer repair.mu.Unlock()
	if len(repair.calls) != 1 {
		t.Fatalf("repair enqueued %d times, want 1", len(repair.calls))
	}
	if repair.calls[0].path != "movie.mkv" || repair.calls[0].segID != "<dead@test>" {
		t.Fatalf("repair call = %+v", repair.calls[0])
	}
}

// Without an enqueuer the recorder keeps working (nil-safe wiring).
func TestPadRecorderWithoutRepairEnqueuer(t *testing.T) {
	meta := &fakePadMetadataStore{holesRecorded: make(chan struct{}, 1)}
	r := newPadRecorder(meta, &fakePadHealthStore{}, nil)

	r.enqueue(testPadEvent("movie.mkv"))
	select {
	case <-meta.holesRecorded:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for hole persistence")
	}
	r.Close()
}
