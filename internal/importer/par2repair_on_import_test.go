package importer

import (
	"context"
	"sync"
	"testing"
)

type recordingRepairEnqueuer struct {
	mu    sync.Mutex
	calls []struct{ path, segID string }
}

func (r *recordingRepairEnqueuer) Enqueue(_ context.Context, filePath, failingSegmentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, struct{ path, segID string }{filePath, failingSegmentID})
}

func (r *recordingRepairEnqueuer) snapshot() []struct{ path, segID string } {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]struct{ path, segID string }(nil), r.calls...)
}

// Degraded NZB files are matched to the virtual paths the import actually
// wrote (names change: sanitizing, deobfuscation, rename-to-nzb-name), so
// matching is by basename.
func TestQueueImportRepairsMatchesWrittenPaths(t *testing.T) {
	enq := &recordingRepairEnqueuer{}
	proc := &Processor{par2Repair: enq}

	written := []string{
		"/movies/Some.Movie/Some.Movie.mkv",
		"/movies/Some.Movie/extra.nfo",
	}
	degraded := map[string]string{
		"Some.Movie.mkv": "<dead@test>",
		"not-imported.mkv": "<other@test>",
	}

	proc.queueImportRepairs(context.Background(), true, written, degraded)

	calls := enq.snapshot()
	if len(calls) != 1 {
		t.Fatalf("enqueued %d times, want 1 (only the written degraded file)", len(calls))
	}
	if calls[0].path != "/movies/Some.Movie/Some.Movie.mkv" || calls[0].segID != "<dead@test>" {
		t.Fatalf("call = %+v", calls[0])
	}
}

func TestQueueImportRepairsSkipsWhenDisabled(t *testing.T) {
	enq := &recordingRepairEnqueuer{}
	proc := &Processor{par2Repair: enq}

	proc.queueImportRepairs(context.Background(), false,
		[]string{"/movies/a.mkv"}, map[string]string{"a.mkv": "<dead@test>"})

	if calls := enq.snapshot(); len(calls) != 0 {
		t.Fatalf("enqueued %d times while disabled, want 0", len(calls))
	}
}

// Nothing degraded and no enqueuer configured are both safe no-ops.
func TestQueueImportRepairsNoOps(t *testing.T) {
	enq := &recordingRepairEnqueuer{}
	proc := &Processor{par2Repair: enq}

	proc.queueImportRepairs(context.Background(), true, []string{"/movies/a.mkv"}, nil)
	if calls := enq.snapshot(); len(calls) != 0 {
		t.Fatalf("enqueued with nothing degraded, want 0 calls")
	}

	// No enqueuer wired at all must not panic.
	(&Processor{}).queueImportRepairs(context.Background(), true,
		[]string{"/movies/a.mkv"}, map[string]string{"a.mkv": "<dead@test>"})
}
