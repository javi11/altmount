package par2repair

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// Config is the repair policy snapshot the service reads per job (so config
// hot-reloads apply to the next job without a restart).
type Config struct {
	Enabled           bool
	MaxRepairRatio    float64
	MaxMemoryMB       int
	MaxConcurrentJobs int
}

// JobStore is the persistence the service needs (satisfied by
// database.Par2RepairRepository).
type JobStore interface {
	Enqueue(ctx context.Context, filePath string, failingSegmentID string) (bool, error)
	ClaimNext(ctx context.Context, now time.Time) (*database.Par2RepairJob, error)
	MarkRepaired(ctx context.Context, id int64) error
	MarkUnrepairable(ctx context.Context, id int64, reason string) error
	MarkRetry(ctx context.Context, id int64, reason string, nextAttempt time.Time) error
	ResetRunning(ctx context.Context) error
}

// MetadataSource reads file metadata and the shared NzbStore (satisfied by
// the adapter over metadata.MetadataService).
type MetadataSource interface {
	ReadFileMetadata(virtualPath string) (*metapb.FileMetadata, error)
	ReadStore(ref string) (*metapb.NzbStore, error)
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
	return s
}

// PatchStore exposes the store for read-path wiring.
func (s *Service) PatchStore() *PatchStore { return s.store }

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
	switch {
	case err == nil:
		s.log.InfoContext(ctx, "PAR2 repair completed", "file", job.FilePath, "duration", time.Since(start))
		if err := s.repo.MarkRepaired(ctx, job.ID); err != nil {
			s.log.ErrorContext(ctx, "Failed to mark repair job repaired", "error", err, "job", job.ID)
		}
	case errors.Is(err, ErrUnrepairable), errors.Is(err, ErrNothingToRepair):
		s.log.WarnContext(ctx, "PAR2 repair not possible", "file", job.FilePath, "reason", err)
		if err := s.repo.MarkUnrepairable(ctx, job.ID, err.Error()); err != nil {
			s.log.ErrorContext(ctx, "Failed to mark repair job unrepairable", "error", err, "job", job.ID)
		}
	case ctx.Err() != nil:
		// Shutdown mid-job: leave it running; ResetRunning reclaims at boot.
		return false
	default:
		if job.Attempts+1 >= maxJobAttempts {
			s.log.ErrorContext(ctx, "PAR2 repair failed permanently after retries",
				"file", job.FilePath, "attempts", job.Attempts+1, "error", err)
			if err := s.repo.MarkUnrepairable(ctx, job.ID, "attempts exhausted: "+err.Error()); err != nil {
				s.log.ErrorContext(ctx, "Failed to mark repair job unrepairable", "error", err, "job", job.ID)
			}
			return true
		}
		delay := min(baseBackoff<<job.Attempts, maxBackoff)
		s.log.WarnContext(ctx, "PAR2 repair failed, will retry",
			"file", job.FilePath, "error", err, "retry_in", delay)
		if err := s.repo.MarkRetry(ctx, job.ID, err.Error(), time.Now().UTC().Add(delay)); err != nil {
			s.log.ErrorContext(ctx, "Failed to schedule repair retry", "error", err, "job", job.ID)
		}
	}
	return true
}

// executeJob is the real pipeline: metadata -> resolve -> run.
func (s *Service) executeJob(ctx context.Context, job *database.Par2RepairJob) error {
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

	deadIDs := deadSegmentIDs(fm, job.FailingSegmentID.String)
	cfg := s.cfg()
	caps := Caps{
		MaxRepairRatio: cfg.MaxRepairRatio,
		MaxMemoryBytes: int64(cfg.MaxMemoryMB) << 20,
	}
	res, err := Resolve(ctx, fm, store, deadIDs, s.fetcher, caps)
	if err != nil {
		return err
	}
	return RunJob(ctx, res.Plan, res.Index, res.Par2Files, s.fetcher, s.store, s.log)
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
