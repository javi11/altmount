package nzbfilesystem

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/holes"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/utils"
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
	GetFileHealth(ctx context.Context, filePath string) (*database.FileHealth, error)
}

// padArrsTrigger is the slice of ARRsRepairService the pad recorder needs;
// narrowed to an interface (same shape as ARRsRepairService, declared
// separately so tests can fake it without importing that type) so a
// degraded-pad event can independently ask the owning ARR instance to
// blocklist+redownload the title without going through the corrupted-file
// repair lifecycle (no safety-folder move, no FILE_STATUS_CORRUPTED - the
// file stays visible and streamable throughout).
type padArrsTrigger interface {
	TriggerFileRescan(ctx context.Context, pathForRescan string, relativePath string, metadataStr *string) error
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
	ch           chan padEvent
	metadata     padMetadataStore
	health       padHealthStore
	coalescer    *RepairCoalescer
	arrs         padArrsTrigger
	configGetter config.ConfigGetter

	stopCh chan struct{}
	stopWg sync.WaitGroup
}

// newPadRecorder constructs a recorder and starts its worker. The worker runs
// for the lifetime of the process; call Close to stop it in tests. arrs and
// configGetter may be nil (e.g. test harnesses building a bare recorder) -
// the immediate-repair-search trigger degrades to a no-op when either is nil,
// same tolerance every other optional collaborator in this file already has.
func newPadRecorder(metadata padMetadataStore, health padHealthStore, coalescer *RepairCoalescer, arrs padArrsTrigger, configGetter config.ConfigGetter) *padRecorder {
	r := &padRecorder{
		ch:           make(chan padEvent, padRecorderQueueSize),
		metadata:     metadata,
		health:       health,
		coalescer:    coalescer,
		arrs:         arrs,
		configGetter: configGetter,
		stopCh:       make(chan struct{}),
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

	r.triggerImmediateRepairSearch(ev)
}

// triggerImmediateRepairSearch asks the owning ARR instance to search for a
// replacement the moment a playback hole is confirmed, instead of waiting for
// the file's next scheduled health check (up to 90 days out for files older
// than 30 days - see health.normalCheckInterval). Deliberately does NOT move
// the file to the corrupted-safety folder or change its FILE_STATUS: the
// current stream must keep reading the degraded-but-playable file undisturbed
// (see the padRecorder doc comment). ARR's own delete+search path only acts
// when the resolved movie/episode file actually matches this virtual file's
// library path (see radarrFileMatchesTarget / its Sonarr equivalent), so
// BearMount's own copy is never touched here - only the ARR-side record.
//
// Runs on its own goroutine so a slow ARR round-trip never stalls this
// recorder's single serialized worker loop (see padRecorder's doc comment);
// bounded by the same "\x00degraded-pad" debounce key already checked above,
// so at most one of these fires per file per debounce window regardless of
// how many holes it accumulates in that window.
func (r *padRecorder) triggerImmediateRepairSearch(ev padEvent) {
	if r.arrs == nil || r.configGetter == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		fh, err := r.health.GetFileHealth(ctx, ev.name)
		if err != nil {
			slog.WarnContext(ctx, "Immediate repair-search: could not read health record, skipping",
				"file", ev.name, "error", err)
			return
		}

		var pathForRescan string
		if fh != nil {
			if p, ok := fh.EffectiveLibraryPath(); ok {
				pathForRescan = p
			}
		}
		if pathForRescan == "" {
			cfg := r.configGetter()
			switch {
			case cfg.Health.LibraryDir != nil && *cfg.Health.LibraryDir != "":
				pathForRescan = utils.JoinAbsPath(*cfg.Health.LibraryDir, ev.name)
			case cfg.Import.ImportDir != nil && *cfg.Import.ImportDir != "":
				pathForRescan = utils.JoinAbsPath(*cfg.Import.ImportDir, ev.name)
			default:
				pathForRescan = utils.JoinAbsPath(cfg.MountPath, ev.name)
			}
		}

		var metadataStr *string
		if fh != nil {
			metadataStr = fh.Metadata
		}

		slog.InfoContext(ctx, "Playback hole confirmed, triggering immediate ARR repair search",
			"file", ev.name, "path_for_rescan", pathForRescan, "total_missing", ev.total)

		if err := r.arrs.TriggerFileRescan(ctx, pathForRescan, ev.name, metadataStr); err != nil {
			slog.WarnContext(ctx, "Immediate repair search failed (file remains degraded, next scheduled check will retry)",
				"file", ev.name, "error", err)
		}
	}()
}
