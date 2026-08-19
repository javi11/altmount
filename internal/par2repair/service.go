package par2repair

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/javi11/nzbparser"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/nzbfile"
)

// Config is the repair policy snapshot the service reads per job (so config
// hot-reloads apply to the next job without a restart).
type Config struct {
	Enabled           bool
	MaxRepairRatio    float64
	MaxMemoryMB       int
	MaxConcurrentJobs int
	// MaxPatchStoreMB bounds the on-disk patch store; <= 0 means unlimited.
	MaxPatchStoreMB int
}

// JobStore is the persistence the service needs (satisfied by
// database.Par2RepairRepository).
type JobStore interface {
	Enqueue(ctx context.Context, filePath string, failingSegmentID string) (bool, error)
	ClaimNext(ctx context.Context, now time.Time) (*database.Par2RepairJob, error)
	Delete(ctx context.Context, id int64) error
	DeleteFinished(ctx context.Context) error
	MarkRetry(ctx context.Context, id int64, reason string, nextAttempt time.Time) error
	AppendDeadSegment(ctx context.Context, id int64, messageID string) error
	EnqueueNzb(ctx context.Context, nzbPath string, failingSegmentID string) (bool, error)
	ResetRunning(ctx context.Context) error
}

// ImportResumer returns an import parked pending a repair to the normal queue,
// or fails it when the damage proved unrepairable. Optional: nil skips resume.
type ImportResumer interface {
	ResumeWaitingRepair(ctx context.Context, nzbPath string) error
	FailWaitingRepair(ctx context.Context, nzbPath string, reason string) error
}

// HealthStore updates a file's health record (satisfied by
// database.HealthRepository). Optional: nil skips health updates.
type HealthStore interface {
	UpdateFileHealth(ctx context.Context, filePath string, status database.HealthStatus, errorMessage *string, sourceNzbPath *string, errorDetails *string, noRetry bool) error
}

// MetadataSource reads file metadata and the shared NzbStore (satisfied by
// the adapter over metadata.MetadataService).
type MetadataSource interface {
	ReadFileMetadata(virtualPath string) (*metapb.FileMetadata, error)
	ReadStore(ref string) (*metapb.NzbStore, error)
	UpdateFileStatus(virtualPath string, status metapb.FileStatus) error
}

// NewMetadataSource adapts a metadata.MetadataService.
func NewMetadataSource(ms *metadata.MetadataService) MetadataSource {
	return metadataSource{ms: ms}
}

type metadataSource struct{ ms *metadata.MetadataService }

func (m metadataSource) ReadFileMetadata(p string) (*metapb.FileMetadata, error) {
	return m.ms.ReadFileMetadata(p)
}
func (m metadataSource) ReadStore(ref string) (*metapb.NzbStore, error) {
	return m.ms.Store().ReadStore(ref)
}
func (m metadataSource) UpdateFileStatus(p string, status metapb.FileStatus) error {
	return m.ms.UpdateFileStatus(p, status)
}

const (
	idlePoll       = 5 * time.Second
	baseBackoff    = time.Minute
	maxBackoff     = 6 * time.Hour
	maxJobAttempts = 8
)

// Service owns the repair queue: triggers enqueue files, worker goroutines
// claim jobs, resolve them against metadata and run the repair pipeline.
type Service struct {
	repo    JobStore
	meta    MetadataSource
	store   *PatchStore
	fetcher ArticleFetcher
	cfg     func() Config
	log     *slog.Logger
	wake    chan struct{}
	health  HealthStore   // optional; nil skips health-record updates
	resumer ImportResumer // optional; releases imports parked pending a repair

	// progress holds live sweep progress per running job ID.
	progressMu sync.Mutex
	progress   map[int64]JobProgressSnapshot

	// resolveNzb plans an NZB-mode repair; replaced in tests. The default
	// parses the NZB from disk and calls ResolveFromNzb.
	resolveNzb func(ctx context.Context, nzbPath string, deadIDs []string) (*Resolution, error)

	// execute runs one claimed job; replaced in tests. The default is
	// (*Service).executeJob.
	execute func(ctx context.Context, job *database.Par2RepairJob) error
}

// NewService wires the repair service. fetcher is typically a PoolFetcher.
func NewService(repo JobStore, meta MetadataSource, fetcher ArticleFetcher, store *PatchStore, cfg func() Config, log *slog.Logger) *Service {
	s := &Service{
		repo:    repo,
		meta:    meta,
		store:   store,
		fetcher: fetcher,
		cfg:     cfg,
		log:     log,
		wake:    make(chan struct{}, 1),
	}
	s.execute = s.executeJob
	s.resolveNzb = s.resolveNzbFromDisk
	return s
}

// PatchStore exposes the store for read-path wiring.
func (s *Service) PatchStore() *PatchStore { return s.store }

// SetHealthStore wires the optional health repository so successful repairs
// flip the file's health record back to healthy. Nil-safe: unset skips it.
func (s *Service) SetHealthStore(h HealthStore) { s.health = h }

// SetImportResumer wires the import queue so NZB-mode repairs release the
// import they were queued for. Nil-safe.
func (s *Service) SetImportResumer(r ImportResumer) { s.resumer = r }

// EnqueueNzb registers a never-imported release for repair, planned from its
// NZB. Used when an import defers an archive set whose volumes have missing
// articles.
func (s *Service) EnqueueNzb(ctx context.Context, nzbPath string, failingSegmentID string) {
	if !s.cfg().Enabled {
		return
	}
	created, err := s.repo.EnqueueNzb(ctx, nzbPath, failingSegmentID)
	if err != nil {
		s.log.ErrorContext(ctx, "Failed to enqueue NZB par2 repair", "error", err, "nzb", nzbPath)
		return
	}
	if created {
		s.log.InfoContext(ctx, "Queued PAR2 repair for deferred import", "nzb", nzbPath)
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// Enqueue registers a file for repair. Safe from any goroutine; a no-op when
// the feature is disabled or an active job already exists. failingSegmentID
// may be empty.
func (s *Service) Enqueue(ctx context.Context, filePath string, failingSegmentID string) {
	if !s.cfg().Enabled {
		return
	}
	created, err := s.repo.Enqueue(ctx, filePath, failingSegmentID)
	if err != nil {
		s.log.ErrorContext(ctx, "Failed to enqueue par2 repair", "error", err, "file", filePath)
		return
	}
	if created {
		s.log.InfoContext(ctx, "Queued PAR2 repair", "file", filePath, "failing_segment", failingSegmentID)
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// Start recovers interrupted jobs and runs worker loops until ctx ends.
func (s *Service) Start(ctx context.Context) {
	if err := s.repo.ResetRunning(ctx); err != nil {
		s.log.ErrorContext(ctx, "Failed to reset running par2 repair jobs", "error", err)
	}
	// Solver arenas from a crashed run are worthless; reclaim the disk.
	if err := os.RemoveAll(s.store.ScratchDir()); err != nil {
		s.log.ErrorContext(ctx, "Failed to clean par2 repair scratch dir", "error", err)
	}
	// Terminal rows written by older versions are history we no longer keep:
	// outcomes live on health records / import queue entries now.
	if err := s.repo.DeleteFinished(ctx); err != nil {
		s.log.ErrorContext(ctx, "Failed to delete finished par2 repair jobs", "error", err)
	}
	workers := max(s.cfg().MaxConcurrentJobs, 1)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.workerLoop(ctx)
		}()
	}
	wg.Wait()
}

func (s *Service) workerLoop(ctx context.Context) {
	ticker := time.NewTicker(idlePoll)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if s.cfg().Enabled {
			if worked := s.runNext(ctx); worked {
				continue // drain the queue without waiting
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}

// runNext claims and executes one job. Returns false when the queue is idle.
func (s *Service) runNext(ctx context.Context) bool {
	job, err := s.repo.ClaimNext(ctx, time.Now().UTC())
	if err != nil {
		s.log.ErrorContext(ctx, "Failed to claim par2 repair job", "error", err)
		return false
	}
	if job == nil {
		return false
	}

	start := time.Now()
	err = s.execute(ctx, job)
	if err != nil && !errors.Is(err, ErrUnrepairable) && !errors.Is(err, ErrNothingToRepair) && ctx.Err() != nil {
		// Shutdown mid-job: leave it running; ResetRunning reclaims at boot.
		return false
	}
	if err == nil {
		s.log.InfoContext(ctx, "PAR2 repair completed", "file", job.FilePath, "duration", time.Since(start))
	}
	s.handleOutcome(ctx, job, err)
	return true
}

// handleOutcome applies the bookkeeping for one executed job. Job rows are
// working state, not history: a terminal outcome is translated to the file's
// health record (file mode) or the import queue entry (NZB mode), then the
// row is deleted. Only retrying jobs keep a row.
func (s *Service) handleOutcome(ctx context.Context, job *database.Par2RepairJob, err error) {
	switch {
	case err == nil:
		s.markFileHealthy(ctx, job.FilePath)
		s.resumeImport(ctx, job)
		s.deleteJob(ctx, job.ID)
		s.pruneStore(ctx)
	case errors.Is(err, ErrUnrepairable), errors.Is(err, ErrNothingToRepair):
		s.log.WarnContext(ctx, "PAR2 repair not possible", "file", job.FilePath, "reason", err)
		s.markFileUnrepairable(ctx, job.FilePath, err.Error())
		s.failImport(ctx, job, err.Error())
		s.deleteJob(ctx, job.ID)
	default:
		if job.Attempts+1 >= maxJobAttempts {
			s.log.ErrorContext(ctx, "PAR2 repair failed permanently after retries",
				"file", job.FilePath, "attempts", job.Attempts+1, "error", err)
			reason := "attempts exhausted: " + err.Error()
			s.markFileUnrepairable(ctx, job.FilePath, reason)
			s.failImport(ctx, job, reason)
			s.deleteJob(ctx, job.ID)
			return
		}
		delay := min(baseBackoff<<job.Attempts, maxBackoff)
		var sweepDead *SweepDeadArticleError
		if errors.As(err, &sweepDead) {
			// The dead article is now a known input to the next plan, so the
			// retry is expected to succeed: persist it and skip the exponent.
			if perr := s.repo.AppendDeadSegment(ctx, job.ID, sweepDead.MessageID); perr != nil {
				s.log.ErrorContext(ctx, "Failed to persist dead segment on repair job",
					"error", perr, "job", job.ID, "message_id", sweepDead.MessageID)
			}
			delay = baseBackoff
		}
		s.log.WarnContext(ctx, "PAR2 repair failed, will retry",
			"file", job.FilePath, "error", err, "retry_in", delay)
		if err := s.repo.MarkRetry(ctx, job.ID, err.Error(), time.Now().UTC().Add(delay)); err != nil {
			s.log.ErrorContext(ctx, "Failed to schedule repair retry", "error", err, "job", job.ID)
		}
	}
}

// deleteJob removes a finished job row; failures are logged, never fatal.
func (s *Service) deleteJob(ctx context.Context, id int64) {
	if err := s.repo.Delete(ctx, id); err != nil {
		s.log.ErrorContext(ctx, "Failed to delete finished repair job", "error", err, "job", id)
	}
}

// markFileUnrepairable records why repair could not fix the file on its
// health record, so the verdict survives the job row's deletion and the file
// stays on the existing ARR/corruption replacement path. No-op for NZB-mode
// jobs (empty filePath): their verdict lands on the import queue entry.
func (s *Service) markFileUnrepairable(ctx context.Context, filePath, reason string) {
	if s.health == nil || filePath == "" {
		return
	}
	if err := s.health.UpdateFileHealth(ctx, filePath, database.HealthStatusCorrupted, &reason, nil, nil, false); err != nil {
		s.log.ErrorContext(ctx, "Failed to record unrepairable verdict on health record",
			"error", err, "file", filePath)
	}
}

// markFileHealthy flips the repaired file's metadata and health record back to
// healthy. Failures are logged, never fatal: the repair itself succeeded.
func (s *Service) markFileHealthy(ctx context.Context, filePath string) {
	if s.meta != nil {
		if err := s.meta.UpdateFileStatus(filePath, metapb.FileStatus_FILE_STATUS_HEALTHY); err != nil {
			s.log.ErrorContext(ctx, "Failed to flip metadata status to healthy after repair",
				"error", err, "file", filePath)
		}
	}
	if s.health != nil {
		note := "restored by PAR2 repair"
		if err := s.health.UpdateFileHealth(ctx, filePath, database.HealthStatusHealthy, nil, nil, &note, false); err != nil {
			s.log.ErrorContext(ctx, "Failed to flip health record to healthy after repair",
				"error", err, "file", filePath)
		}
	}
}

// pruneStore enforces the configured patch-store size cap after a successful
// job. Errors are logged, never fatal.
func (s *Service) pruneStore(ctx context.Context) {
	maxBytes := int64(s.cfg().MaxPatchStoreMB) << 20
	if err := s.store.Prune(maxBytes); err != nil {
		s.log.ErrorContext(ctx, "Failed to prune patch store", "error", err)
	}
}

// executeJob is the real pipeline: metadata -> resolve -> run.
func (s *Service) executeJob(ctx context.Context, job *database.Par2RepairJob) error {
	// NZB-mode: the release was never imported, so there is no metadata to
	// read — plan straight from the NZB instead.
	if job.NzbPath.Valid && job.NzbPath.String != "" {
		return s.executeNzbJob(ctx, job)
	}
	fm, err := s.meta.ReadFileMetadata(job.FilePath)
	if err != nil {
		return err
	}
	if fm == nil {
		return errors.Join(ErrUnrepairable, errors.New("file metadata not found"))
	}
	var store *metapb.NzbStore
	if fm.StoreRef != "" {
		if store, err = s.meta.ReadStore(fm.StoreRef); err != nil {
			return err
		}
	}

	deadIDs := mergeDeadIDs(deadSegmentIDs(fm, job.FailingSegmentID.String), job.DeadSegments())
	cfg := s.cfg()
	caps := Caps{
		MaxRepairRatio: cfg.MaxRepairRatio,
		MaxMemoryBytes: int64(cfg.MaxMemoryMB) << 20,
	}
	res, err := Resolve(ctx, fm, store, deadIDs, s.fetcher, caps)
	if err != nil {
		return err
	}
	defer s.clearProgress(job.ID)
	return RunJob(ctx, res.Plan, res.Index, res.Par2Files, s.fetcher, s.store, s.log,
		WithProgress(func(done, total int) { s.setProgress(job.ID, done, total) }))
}

// JobProgressSnapshot is a running job's sweep progress.
type JobProgressSnapshot struct {
	DoneArticles  int
	TotalArticles int
}

// Progress returns the live sweep progress of a running job, when known.
func (s *Service) Progress(jobID int64) (JobProgressSnapshot, bool) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	p, ok := s.progress[jobID]
	return p, ok
}

func (s *Service) setProgress(jobID int64, done, total int) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if s.progress == nil {
		s.progress = make(map[int64]JobProgressSnapshot)
	}
	s.progress[jobID] = JobProgressSnapshot{DoneArticles: done, TotalArticles: total}
}

func (s *Service) clearProgress(jobID int64) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	delete(s.progress, jobID)
}

// executeNzbJob runs a repair planned from an NZB file.
func (s *Service) executeNzbJob(ctx context.Context, job *database.Par2RepairJob) error {
	deadIDs := mergeDeadIDs(nil, job.DeadSegments())
	if job.FailingSegmentID.Valid && job.FailingSegmentID.String != "" {
		deadIDs = mergeDeadIDs([]string{job.FailingSegmentID.String}, deadIDs)
	}
	res, err := s.resolveNzb(ctx, job.NzbPath.String, deadIDs)
	if err != nil {
		return err
	}
	defer s.clearProgress(job.ID)
	return RunJob(ctx, res.Plan, res.Index, res.Par2Files, s.fetcher, s.store, s.log,
		WithProgress(func(done, total int) { s.setProgress(job.ID, done, total) }))
}

// resolveNzbFromDisk parses the NZB at nzbPath and plans a repair from it.
func (s *Service) resolveNzbFromDisk(ctx context.Context, nzbPath string, deadIDs []string) (*Resolution, error) {
	rc, err := nzbfile.Open(nzbPath)
	if err != nil {
		return nil, fmt.Errorf("%w: open NZB %s: %v", ErrUnrepairable, nzbPath, err)
	}
	defer func() { _ = rc.Close() }()

	n, err := nzbparser.Parse(rc)
	if err != nil {
		return nil, fmt.Errorf("%w: parse NZB %s: %v", ErrUnrepairable, nzbPath, err)
	}
	cfg := s.cfg()
	return ResolveFromNzb(ctx, n, deadIDs, s.fetcher, Caps{
		MaxRepairRatio: cfg.MaxRepairRatio,
		MaxMemoryBytes: int64(cfg.MaxMemoryMB) << 20,
	})
}

// resumeImport returns a parked import to the queue after its repair landed.
func (s *Service) resumeImport(ctx context.Context, job *database.Par2RepairJob) {
	if s.resumer == nil || !job.NzbPath.Valid || job.NzbPath.String == "" {
		return
	}
	if err := s.resumer.ResumeWaitingRepair(ctx, job.NzbPath.String); err != nil {
		s.log.ErrorContext(ctx, "Failed to resume import after repair", "error", err, "nzb", job.NzbPath.String)
		return
	}
	s.log.InfoContext(ctx, "Resuming import after successful PAR2 repair", "nzb", job.NzbPath.String)
}

// failImport fails a parked import whose repair proved impossible.
func (s *Service) failImport(ctx context.Context, job *database.Par2RepairJob, reason string) {
	if s.resumer == nil || !job.NzbPath.Valid || job.NzbPath.String == "" {
		return
	}
	if err := s.resumer.FailWaitingRepair(ctx, job.NzbPath.String, reason); err != nil {
		s.log.ErrorContext(ctx, "Failed to fail parked import", "error", err, "nzb", job.NzbPath.String)
	}
}

// mergeDeadIDs appends extra onto base, skipping duplicates, preserving order.
func mergeDeadIDs(base, extra []string) []string {
	out := base
	for _, id := range extra {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// deadSegmentIDs collects the trigger's failing segment plus every persisted
// known hole, mapped from segment indexes to message IDs.
func deadSegmentIDs(fm *metapb.FileMetadata, failing string) []string {
	ids := []string{}
	if failing != "" {
		ids = append(ids, failing)
	}
	for _, run := range fm.KnownHoles {
		for i := run.StartSegment; i < run.StartSegment+run.Count; i++ {
			if i >= 0 && int(i) < len(fm.SegmentData) {
				ids = append(ids, fm.SegmentData[i].Id)
			}
		}
	}
	return ids
}
