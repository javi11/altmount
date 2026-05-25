package nzbfilesystem

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// repairJob is a single in-flight (or just-completed) whole-file PAR2 repair.
// done is closed when the job finishes; err is then valid (nil == healed).
type repairJob struct {
	done chan struct{}
	err  error
}

// RepairManager coordinates PAR2 self-heal jobs across the readers of a file so
// that concurrent corrupt reads share one whole-file reconstruction instead of
// each launching its own (a full-file download per reader). It also remembers a
// successful heal briefly so repeated holes don't re-run the repair back to
// back. A single instance is shared by every file handle.
type RepairManager struct {
	svc RepairService
	log *slog.Logger

	timeout   time.Duration // hard cap on one RepairFile attempt
	resultTTL time.Duration // window in which a finished heal is reused

	mu       sync.Mutex
	jobs     map[string]*repairJob // path -> in-flight job
	finished map[string]time.Time  // path -> last successful heal time
}

// NewRepairManager constructs a manager driving svc. log defaults to
// slog.Default() when nil.
func NewRepairManager(svc RepairService, log *slog.Logger) *RepairManager {
	if log == nil {
		log = slog.Default()
	}
	return &RepairManager{
		svc:       svc,
		log:       log.With("component", "par2-repair-manager"),
		timeout:   defaultSelfHealTimeout,
		resultTTL: 30 * time.Second,
		jobs:      make(map[string]*repairJob),
		finished:  make(map[string]time.Time),
	}
}

// Start returns the in-flight repair job for path, launching one if none is
// running. Concurrent callers for the same path receive the same job, so the
// underlying whole-file reconstruction runs at most once at a time.
func (m *RepairManager) Start(path string) *repairJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[path]; ok {
		return j
	}
	j := &repairJob{done: make(chan struct{})}
	m.jobs[path] = j
	go m.run(path, j)
	return j
}

func (m *RepairManager) run(path string, j *repairJob) {
	// A background-derived context so a cancelled originating read does not
	// abort a heal that other readers are waiting on.
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	j.err = m.svc.RepairFile(ctx, path)
	close(j.done)

	m.mu.Lock()
	delete(m.jobs, path)
	if j.err == nil {
		m.finished[path] = time.Now()
	}
	cutoff := time.Now().Add(-m.resultTTL)
	for p, t := range m.finished {
		if t.Before(cutoff) {
			delete(m.finished, p)
		}
	}
	m.mu.Unlock()

	if j.err != nil {
		m.log.InfoContext(ctx, "PAR2 self-heal job failed", "file", path, "reason", j.err)
	} else {
		m.log.InfoContext(ctx, "PAR2 self-heal job restored file", "file", path)
	}
}

// HealedRecently reports whether path was successfully healed within resultTTL,
// letting a read skip straight to a retry without waiting on a fresh job.
func (m *RepairManager) HealedRecently(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.finished[path]
	return ok && time.Since(t) < m.resultTTL
}

// Wait blocks until j completes or ctx is done. It returns the job's repair
// error (nil == healed) on completion, or ctx.Err() if the wait deadline (the
// caller's block-on-repair budget) elapses first while the job keeps running.
func (m *RepairManager) Wait(ctx context.Context, j *repairJob) error {
	select {
	case <-j.done:
		return j.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
