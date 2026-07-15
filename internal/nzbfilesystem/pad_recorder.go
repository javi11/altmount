package nzbfilesystem

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/holes"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// padMetadataStore is the slice of metadata.MetadataService the pad recorder
// needs; narrowed to an interface so tests can fake it.
type padMetadataStore interface {
	AddKnownHoles(virtualPath string, runs []holes.Run) error
	UpdateFileStatus(virtualPath string, status metapb.FileStatus) error
}

// padHealthStore is the slice of database.HealthRepository the pad recorder
// needs; narrowed to an interface so tests can fake it.
type padHealthStore interface {
	UpdateFileHealthScheduled(ctx context.Context, filePath string, status database.HealthStatus, errorMessage *string, sourceNzbPath *string, errorDetails *string, noRetry bool, scheduledAt time.Time) error
}

// padEvent carries everything a degraded-pad record needs as plain values.
// Events must never reference handle state (mvf.meta in particular): the
// handle that produced an event may be Closed before the event is processed.
type padEvent struct {
	name          string
	segIndex      int
	sourceNzbPath string
	fileSize      int64
	total         int
	longest       int
	totalSegments int
	segBytes      int64
}

// padRecorderQueueSize bounds the pending-event buffer. Pads are debounced
// per file and capped by the padding thresholds, so a burst that overflows
// this is pathological; overflow drops the event (best-effort bookkeeping).
const padRecorderQueueSize = 128

// padRecorder persists degraded-pad events off the download hot path: hole
// map merged into metadata, FILE_STATUS_DEGRADED (stays visible and
// streamable), health record degraded. Deliberately NO repair trigger, NO
// safety-folder move and NO masking-counter increment — the file still plays.
// Status writes are debounced per file so a burst of pads writes once per
// window; the hole itself is always persisted (idempotent merge).
//
// A single process-lived worker serializes the writes, so events outlive the
// file handles that produced them and Close()'ing a handle never races the
// recording. Compare RepairCoalescer, which owns the repair side the same way.
type padRecorder struct {
	ch        chan padEvent
	metadata  padMetadataStore
	health    padHealthStore
	coalescer *RepairCoalescer

	stopCh chan struct{}
	stopWg sync.WaitGroup
}

// newPadRecorder constructs a recorder and starts its worker. The worker runs
// for the lifetime of the process; call Close to stop it in tests.
func newPadRecorder(metadata padMetadataStore, health padHealthStore, coalescer *RepairCoalescer) *padRecorder {
	r := &padRecorder{
		ch:        make(chan padEvent, padRecorderQueueSize),
		metadata:  metadata,
		health:    health,
		coalescer: coalescer,
		stopCh:    make(chan struct{}),
	}
	r.stopWg.Add(1)
	go r.run()
	return r
}

// enqueue hands an event to the worker without ever blocking the caller
// (onHole runs on download goroutines). On a full buffer the event is
// dropped with a warning. Safe on a nil receiver (tests build
// MetadataVirtualFile literals without a recorder).
func (r *padRecorder) enqueue(ev padEvent) {
	if r == nil {
		return
	}
	select {
	case r.ch <- ev:
	default:
		slog.Warn("Degraded-pad recorder queue full, dropping event",
			"file", ev.name, "segment_index", ev.segIndex)
	}
}

// Close stops the worker after draining any buffered events.
func (r *padRecorder) Close() {
	close(r.stopCh)
	r.stopWg.Wait()
}

func (r *padRecorder) run() {
	defer r.stopWg.Done()
	for {
		select {
		case ev := <-r.ch:
			r.record(ev)
		case <-r.stopCh:
			for {
				select {
				case ev := <-r.ch:
					r.record(ev)
				default:
					return
				}
			}
		}
	}
}

// record persists one pad event. The recover keeps a single bad event from
// killing the worker (and, since this is a bare goroutine, the process).
func (r *padRecorder) record(ev padEvent) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic recording degraded pad", "file", ev.name, "panic", rec)
		}
	}()

	// Always persist the hole so the next open pre-pads it without a fetch.
	if err := r.metadata.AddKnownHoles(ev.name, []holes.Run{{Start: ev.segIndex, Count: 1}}); err != nil {
		slog.Warn("Failed to persist known hole", "file", ev.name, "error", err)
	}

	// Distinct debounce key from the repair path so pads never consume a
	// repair-trigger token.
	if !r.coalescer.ShouldTrigger(ev.name + "\x00degraded-pad") {
		return
	}

	// Process-scoped context: the originating handle may already be closed.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := r.metadata.UpdateFileStatus(ev.name, metapb.FileStatus_FILE_STATUS_DEGRADED); err != nil {
		slog.WarnContext(ctx, "Failed to update metadata status to degraded", "file", ev.name, "error", err)
	}

	details := database.HealthErrorDetails{
		ErrorType:       "ArticleNotFound",
		MissingArticles: ev.total,
		TotalArticles:   ev.totalSegments,
		PlaybackImpact: &holes.Impact{
			Verdict:       holes.VerdictDegraded,
			TotalMissing:  ev.total,
			LongestRun:    ev.longest,
			TotalSegments: ev.totalSegments,
			PaddedRatio:   paddedRatio(ev.total, ev.segBytes, ev.fileSize),
		},
	}
	errorMsg := "missing segments zero-filled during streaming"
	var sourceNzbPath *string
	if ev.sourceNzbPath != "" {
		sourceNzbPath = &ev.sourceNzbPath
	}

	slog.InfoContext(ctx, "Zero-filled missing segment during streaming, file marked degraded",
		"file", ev.name,
		"total_missing", ev.total,
		"longest_run", ev.longest)

	if err := r.health.UpdateFileHealthScheduled(ctx,
		ev.name,
		database.HealthStatusDegraded,
		&errorMsg,
		sourceNzbPath,
		details.Marshal(),
		false, // no immediate scheduling — periodic re-check refines the verdict
		time.Now().UTC(),
	); err != nil {
		slog.WarnContext(ctx, "Failed to record degraded status for padded file", "file", ev.name, "error", err)
	}
}
