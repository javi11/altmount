# PAR2 Repair Cancellation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user stop a queued or in-flight PAR2 repair from the Health page, with the job's transient scratch/tmp artifacts provably gone before the API responds.

**Architecture:** `par2repair.Service` keeps an in-process registry of the jobs it is running, each with a `context.CancelFunc` and a `done` channel. `Cancel` cancels the job context and waits for the worker goroutine to unwind — which runs `RunJob`'s arena `defer`s, deleting the mmap scratch files — then sweeps orphaned scratch/tmp files. The worker itself performs the terminal bookkeeping (fail parked import, delete row) so that logic has one owner. Two DELETE routes expose it; the Health page's PAR2 Repairs card gets per-row and header cancel buttons behind the app's existing confirm modal.

**Tech Stack:** Go 1.24 (stdlib `context`/`sync`, `log/slog`, Fiber v2, sqlite3 for tests), React 19 + TypeScript, TanStack Query, DaisyUI, lucide-react, Biome (`bun run check`).

**Spec:** `docs/superpowers/specs/2026-08-19-par2-repair-cancel-design.md`

## Global Constraints

- **No database migration.** Cancellation adds no persisted state; `Get` is a plain `SELECT` on the existing `par2_repair_jobs` table.
- **Cancel is a plain stop.** Never write a health record or metadata status on cancel — the file is left exactly as it was so the next stream read or health check may legitimately re-queue the repair.
- **Never delete `.patch` files.** They are valid repaired bytes already served on the read path. Cleanup covers only `.scratch` contents and `.tmp-*` files.
- **Unwind wait bound:** 30 seconds. On timeout `Cancel` returns an error and leaves the row untouched, so a retry is safe.
- **Parked imports:** an NZB-mode job's import is failed with the exact reason string `repair cancelled by user`.
- **Logging:** `slog` context methods only (`InfoContext`, `WarnContext`, `ErrorContext`), structured key-value pairs. All cleanup failures are logged, never fatal.
- **API responses:** use the builders in `internal/api/response.go` — never inline `c.Status(...).JSON(...)`.
- **Commit messages:** Conventional Commits (`feat:`, `fix:`, `test:`, `refactor:`), required on this branch.
- **Buttons:** every JSX `<button>` carries an explicit `type` attribute; icon-only buttons carry `aria-label`.

---

### Task 1: PatchStore cleanup primitives

Two idempotent artifact sweepers. `RemoveScratch` drops the whole solver-arena directory; `SweepTempFiles` removes half-written `.tmp-*` files left by an interrupted `Put`. Neither may touch a `.patch` file.

**Files:**
- Modify: `internal/par2repair/patchstore.go` (append after `Prune`, before `path`)
- Test: `internal/par2repair/patchstore_test.go` (append)

**Interfaces:**
- Consumes: nothing (first task).
- Produces: `func (p *PatchStore) RemoveScratch() error` and `func (p *PatchStore) SweepTempFiles() error`, both safe to call when the root does not exist.

- [ ] **Step 1: Write the failing tests**

Append to `internal/par2repair/patchstore_test.go`:

```go
func TestPatchStoreRemoveScratch(t *testing.T) {
	store := NewPatchStore(t.TempDir())
	if err := store.Put("<keep@x>", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	scratch := store.ScratchDir()
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, ".par2repair-1.mem"), []byte("arena"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.RemoveScratch(); err != nil {
		t.Fatalf("RemoveScratch: %v", err)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("scratch dir still present, stat err = %v", err)
	}
	if _, ok := store.Get("<keep@x>"); !ok {
		t.Fatal("RemoveScratch deleted a stored patch")
	}
	// Idempotent: a second call on a missing dir is not an error.
	if err := store.RemoveScratch(); err != nil {
		t.Fatalf("second RemoveScratch: %v", err)
	}
}

func TestPatchStoreSweepTempFiles(t *testing.T) {
	root := t.TempDir()
	store := NewPatchStore(root)
	if err := store.Put("<keep@x>", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	// A leftover temp file in the same fanned-out dir as the real patch.
	dir := filepath.Join(root, "ab")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, ".tmp-orphan")
	if err := os.WriteFile(tmp, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.SweepTempFiles(); err != nil {
		t.Fatalf("SweepTempFiles: %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("temp file survived, stat err = %v", err)
	}
	if _, ok := store.Get("<keep@x>"); !ok {
		t.Fatal("SweepTempFiles deleted a stored patch")
	}
}

func TestPatchStoreSweepersOnMissingRoot(t *testing.T) {
	store := NewPatchStore(filepath.Join(t.TempDir(), "never-created"))
	if err := store.RemoveScratch(); err != nil {
		t.Fatalf("RemoveScratch on missing root: %v", err)
	}
	if err := store.SweepTempFiles(); err != nil {
		t.Fatalf("SweepTempFiles on missing root: %v", err)
	}
}
```

Verify `internal/par2repair/patchstore_test.go` already imports `os`, `path/filepath` and `testing`; add whichever are missing to its import block.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/par2repair/ -run 'TestPatchStore(RemoveScratch|SweepTempFiles|SweepersOnMissingRoot)' -v
```

Expected: FAIL to build — `store.RemoveScratch undefined (type *PatchStore has no field or method RemoveScratch)`.

- [ ] **Step 3: Implement the sweepers**

Insert into `internal/par2repair/patchstore.go` between `Prune` and `path`:

```go
// RemoveScratch deletes the solver-arena scratch directory. Arenas only matter
// while a job runs, so removing them between jobs is always safe. Idempotent:
// a missing directory is not an error.
func (p *PatchStore) RemoveScratch() error {
	if err := os.RemoveAll(p.ScratchDir()); err != nil {
		return fmt.Errorf("par2repair: remove scratch dir: %w", err)
	}
	return nil
}

// SweepTempFiles removes half-written patch temp files left behind when a Put
// was interrupted (kill -9, power loss). A live Put's temp file is only visible
// for the moment before its rename, so callers must run this when no job is
// writing. Never touches .patch files.
func (p *PatchStore) SweepTempFiles() error {
	err := filepath.WalkDir(p.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A file deleted mid-walk (concurrent repair) is fine.
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasPrefix(d.Name(), ".tmp-") {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // nothing stored yet
		}
		return fmt.Errorf("par2repair: sweep patch temp files: %w", err)
	}
	return nil
}
```

Add `"strings"` to the import block of `internal/par2repair/patchstore.go` (`errors`, `fmt`, `io/fs`, `os`, `path/filepath` are already imported).

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/par2repair/ -run 'TestPatchStore' -v
```

Expected: PASS, including the pre-existing `Prune`/`Put`/`Get` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/par2repair/patchstore.go internal/par2repair/patchstore_test.go
git commit -m "feat(par2repair): add scratch and temp-file sweepers to PatchStore"
```

---

### Task 2: Repository lookup by ID

`Cancel` needs the row for a job no worker holds, to decide whether it exists at all and whether it has a parked import to fail.

**Files:**
- Modify: `internal/database/par2repair_repository.go` (append after `List`, before `ResetRunning`)
- Test: `internal/database/par2repair_repository_test.go` (append)

**Interfaces:**
- Consumes: nothing.
- Produces: `func (r *Par2RepairRepository) Get(ctx context.Context, id int64) (*Par2RepairJob, error)` — returns `(nil, nil)` when no row has that id.

- [ ] **Step 1: Write the failing test**

Append to `internal/database/par2repair_repository_test.go`, reusing that file's existing `newPar2RepairRepo(t)` helper (it returns `(*Par2RepairRepository, *sql.DB)`); do not add a second constructor.

```go
func TestPar2RepairGet(t *testing.T) {
	repo, _ := newPar2RepairRepo(t)
	ctx := context.Background()
	if _, err := repo.Enqueue(ctx, "/movies/a.mkv", "<seg@x>"); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.List(ctx, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: err=%v rows=%d", err, len(rows))
	}

	got, err := repo.Get(ctx, rows[0].ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil for an existing job")
	}
	if got.ID != rows[0].ID || got.FilePath != "/movies/a.mkv" {
		t.Fatalf("got = %+v, want id=%d file=/movies/a.mkv", got, rows[0].ID)
	}
	if !got.FailingSegmentID.Valid || got.FailingSegmentID.String != "<seg@x>" {
		t.Fatalf("failing segment = %+v, want <seg@x>", got.FailingSegmentID)
	}

	missing, err := repo.Get(ctx, 99999)
	if err != nil {
		t.Fatalf("Get(missing): unexpected error %v", err)
	}
	if missing != nil {
		t.Fatalf("Get(missing) = %+v, want nil", missing)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/database/ -run TestPar2RepairGet -v
```

Expected: FAIL to build — `repo.Get undefined`.

- [ ] **Step 3: Implement Get**

Insert into `internal/database/par2repair_repository.go` after `List` and before `ResetRunning`:

```go
// Get returns one job by ID, or (nil, nil) when no such row exists.
func (r *Par2RepairRepository) Get(ctx context.Context, id int64) (*Par2RepairJob, error) {
	job := &Par2RepairJob{}
	err := r.db.QueryRowContext(ctx, `
		SELECT id, file_path, nzb_path, status, attempts, last_error, failing_segment_id,
		       dead_segment_ids, next_attempt_at, started_at, finished_at, created_at, updated_at
		FROM par2_repair_jobs
		WHERE id = ?`, id).Scan(
		&job.ID, &job.FilePath, &job.NzbPath, &job.Status, &job.Attempts, &job.LastError,
		&job.FailingSegmentID, &job.DeadSegmentIDs, &job.NextAttemptAt,
		&job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get par2 repair job: %w", err)
	}
	return job, nil
}
```

`context`, `database/sql`, `errors` and `fmt` are already imported in that file. If `r.db` (a `DBQuerier`) has no `QueryRowContext`, use `QueryContext` with the same SQL and scan the single row inside `if rows.Next()`, returning `(nil, nil)` when it yields nothing.

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/database/ -run TestPar2Repair -v
```

Expected: PASS for `TestPar2RepairGet` and the pre-existing `TestPar2Repair*` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/database/par2repair_repository.go internal/database/par2repair_repository_test.go
git commit -m "feat(database): look up a PAR2 repair job by ID"
```

---

### Task 3: Service cancel registry and cancelled outcome

The core. `runNext` gains a per-job cancellable context registered in a map; `handleOutcome` gains a cancelled branch ahead of the retry branch; `Cancel` drives it and sweeps artifacts afterwards.

**Files:**
- Modify: `internal/par2repair/service.go` (`JobStore` interface, `Service` struct, `runNext`, `handleOutcome`; add `Cancel`, `sweepArtifacts`, registry helpers)
- Test: `internal/par2repair/service_test.go` (append)

**Interfaces:**
- Consumes: `(*PatchStore).RemoveScratch()`, `(*PatchStore).SweepTempFiles()` (Task 1); `(*database.Par2RepairRepository).Get(ctx, id)` (Task 2).
- Produces:
  - `var ErrJobNotFound = errors.New("par2repair: job not found")`
  - `func (s *Service) Cancel(ctx context.Context, jobID int64) error`
  - `JobStore` interface gains `Get(ctx context.Context, id int64) (*database.Par2RepairJob, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/par2repair/service_test.go`:

```go
// recordingResumer is an ImportResumer fake that records both calls.
type recordingResumer struct {
	resumed []string
	failed  []struct{ nzb, reason string }
}

func (r *recordingResumer) ResumeWaitingRepair(_ context.Context, nzbPath string) error {
	r.resumed = append(r.resumed, nzbPath)
	return nil
}

func (r *recordingResumer) FailWaitingRepair(_ context.Context, nzbPath, reason string) error {
	r.failed = append(r.failed, struct{ nzb, reason string }{nzbPath, reason})
	return nil
}

// Cancelling a running job must stop it, delete the row without scheduling a
// retry, and return only after the job goroutine has unwound (which is what
// runs RunJob's arena cleanup defers).
func TestServiceCancelRunningJob(t *testing.T) {
	repo := newTestRepo(t)
	s := testService(t, repo, true)
	healthStore := &recordingHealth{}
	s.SetHealthStore(healthStore)

	started := make(chan struct{})
	unwound := make(chan struct{})
	s.execute = func(ctx context.Context, _ *database.Par2RepairJob) error {
		close(started)
		<-ctx.Done()
		close(unwound)
		return ctx.Err()
	}

	s.Enqueue(context.Background(), "/movies/a.mkv", "")
	go s.runNext(context.Background())
	<-started

	rows, err := repo.List(context.Background(), 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list before cancel: err=%v rows=%d", err, len(rows))
	}
	jobID := rows[0].ID
	if err := s.Cancel(context.Background(), jobID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	select {
	case <-unwound:
	default:
		t.Fatal("Cancel returned before the job goroutine unwound")
	}

	rows, err = repo.List(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %+v, want none after cancel", rows)
	}
	if len(healthStore.calls) != 0 {
		t.Fatalf("health calls = %+v, want none: cancel is a plain stop", healthStore.calls)
	}
	if _, ok := s.Progress(jobID); ok {
		t.Fatal("progress not cleared after cancel")
	}
}

// An NZB-mode job parks an import; cancelling must release it as failed so it
// cannot wait in waiting_repair forever.
func TestServiceCancelRunningNzbJobFailsParkedImport(t *testing.T) {
	repo := newTestRepo(t)
	s := testService(t, repo, true)
	resumer := &recordingResumer{}
	s.SetImportResumer(resumer)

	started := make(chan struct{})
	s.execute = func(ctx context.Context, _ *database.Par2RepairJob) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}

	if _, err := repo.EnqueueNzb(context.Background(), "/nzbs/rel.nzb", "<dead@x>"); err != nil {
		t.Fatal(err)
	}
	go s.runNext(context.Background())
	<-started

	rows, err := repo.List(context.Background(), 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: err=%v rows=%d", err, len(rows))
	}
	if err := s.Cancel(context.Background(), rows[0].ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if len(resumer.resumed) != 0 {
		t.Fatalf("resumed = %v, want none on cancel", resumer.resumed)
	}
	if len(resumer.failed) != 1 ||
		resumer.failed[0].nzb != "/nzbs/rel.nzb" ||
		resumer.failed[0].reason != cancelReason {
		t.Fatalf("failed = %+v, want one /nzbs/rel.nzb with the cancel reason", resumer.failed)
	}
}

// A pending job is held by no worker: Cancel deletes it directly.
func TestServiceCancelPendingJob(t *testing.T) {
	repo := newTestRepo(t)
	s := testService(t, repo, true)

	s.Enqueue(context.Background(), "/movies/a.mkv", "")
	rows, err := repo.List(context.Background(), 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: err=%v rows=%d", err, len(rows))
	}
	if err := s.Cancel(context.Background(), rows[0].ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	rows, err = repo.List(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %+v, want none after cancelling a pending job", rows)
	}
}

func TestServiceCancelUnknownJob(t *testing.T) {
	repo := newTestRepo(t)
	s := testService(t, repo, true)
	if err := s.Cancel(context.Background(), 4242); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("Cancel(unknown) = %v, want ErrJobNotFound", err)
	}
}

// Cancel removes the solver scratch directory once nothing is running.
func TestServiceCancelSweepsScratch(t *testing.T) {
	repo := newTestRepo(t)
	s := testService(t, repo, true)
	scratch := s.store.ScratchDir()
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, ".par2repair-1.mem"), []byte("arena"), 0o644); err != nil {
		t.Fatal(err)
	}

	s.Enqueue(context.Background(), "/movies/a.mkv", "")
	rows, err := repo.List(context.Background(), 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: err=%v rows=%d", err, len(rows))
	}
	if err := s.Cancel(context.Background(), rows[0].ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("scratch dir survived cancel, stat err = %v", err)
	}
}
```

Add `"os"` and `"path/filepath"` to the import block of `internal/par2repair/service_test.go` (`context`, `database/sql`, `errors`, `fmt`, `strings`, `testing`, `time` are already there).

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/par2repair/ -run 'TestServiceCancel' -v
```

Expected: FAIL to build — `s.Cancel undefined`, `undefined: ErrJobNotFound`, `undefined: cancelReason`.

- [ ] **Step 3: Extend the JobStore interface and Service struct**

In `internal/par2repair/service.go`, add `Get` to the `JobStore` interface (after `ClaimNext`):

```go
	Get(ctx context.Context, id int64) (*database.Par2RepairJob, error)
```

Add above the `Service` struct:

```go
// ErrJobNotFound reports that no active repair job has the requested ID —
// either it never existed or it finished moments earlier.
var ErrJobNotFound = errors.New("par2repair: job not found")

// cancelReason is recorded on an import parked by an NZB-mode job whose repair
// the user cancelled.
const cancelReason = "repair cancelled by user"

// cancelUnwindTimeout bounds how long Cancel waits for a running job to
// unwind. Unwinding runs RunJob's arena cleanup defers, so Cancel waits rather
// than returning while scratch files are still mapped.
const cancelUnwindTimeout = 30 * time.Second

// runningJob is the live handle on a job a worker is executing, so Cancel can
// stop it and wait for its cleanup defers to run.
type runningJob struct {
	cancel    context.CancelFunc
	done      chan struct{} // closed once the worker has unwound
	cancelled bool          // set by Cancel, read by the worker
}
```

Add to the `Service` struct, next to the `progress` fields:

```go
	// running holds the live handle on each job a worker is executing.
	runningMu sync.Mutex
	running   map[int64]*runningJob
```

- [ ] **Step 4: Register the job in runNext and honour the cancelled flag**

Replace the body of `runNext` in `internal/par2repair/service.go` from the `start := time.Now()` line through `return true` with:

```go
	start := time.Now()
	jobCtx, cancel := context.WithCancel(ctx)
	rj := s.registerRunning(job.ID, cancel)
	// Deferred so every return path retires the job, and so done closes only
	// after the bookkeeping below has run.
	defer s.finishRunning(job.ID, rj)

	err = s.execute(jobCtx, job)

	if s.wasCancelled(job.ID) {
		// Cancel owns the user-visible verdict; skip retry bookkeeping so the
		// row cannot be resurrected.
		s.handleCancelled(context.WithoutCancel(ctx), job)
		return true
	}
	if err != nil && !errors.Is(err, ErrUnrepairable) && !errors.Is(err, ErrNothingToRepair) && ctx.Err() != nil {
		// Shutdown mid-job: leave it running; ResetRunning reclaims at boot.
		return false
	}
	if err == nil {
		s.log.InfoContext(ctx, "PAR2 repair completed", "file", job.FilePath, "duration", time.Since(start))
	}
	s.handleOutcome(ctx, job, err)
	return true
```

Add the registry helpers and the cancelled outcome after `handleOutcome`:

```go
// registerRunning publishes a running job so Cancel can reach it.
func (s *Service) registerRunning(id int64, cancel context.CancelFunc) *runningJob {
	rj := &runningJob{cancel: cancel, done: make(chan struct{})}
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	if s.running == nil {
		s.running = make(map[int64]*runningJob)
	}
	s.running[id] = rj
	return rj
}

// wasCancelled reports whether Cancel flagged this job, leaving it registered
// so Cancel keeps waiting until the bookkeeping is done.
func (s *Service) wasCancelled(id int64) bool {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	rj := s.running[id]
	return rj != nil && rj.cancelled
}

// finishRunning retires the job: it releases the job context, drops the
// registry entry, and only then closes done. Closing done last is what makes
// Cancel's wait mean "the job unwound AND its bookkeeping landed" — close it
// any earlier and Cancel could return while the row still exists.
func (s *Service) finishRunning(id int64, rj *runningJob) {
	rj.cancel()
	s.runningMu.Lock()
	delete(s.running, id)
	s.runningMu.Unlock()
	close(rj.done)
}

// handleCancelled applies the bookkeeping for a user-cancelled job. Cancel is
// a plain stop: no health record and no metadata status are written, so the
// file is left exactly as it was and a later read may queue the repair again.
// An NZB-mode job's parked import is failed, since nothing else would ever
// release it.
func (s *Service) handleCancelled(ctx context.Context, job *database.Par2RepairJob) {
	s.log.InfoContext(ctx, "PAR2 repair cancelled", "file", job.FilePath, "job", job.ID)
	s.failImport(ctx, job, cancelReason)
	s.deleteJob(ctx, job.ID)
	s.clearProgress(job.ID)
}
```

`context.WithoutCancel` is used because the worker's `ctx` may itself be cancelled during shutdown, and the cancelled job's bookkeeping must still land.

- [ ] **Step 5: Implement Cancel and sweepArtifacts**

Append to `internal/par2repair/service.go`:

```go
// Cancel stops a queued or in-flight repair and cleans the transient artifacts
// it generated. For a running job it cancels the job's context and waits for
// the worker to unwind, so the solver's mmap scratch files are gone before
// Cancel returns; the worker then deletes the row and releases any parked
// import. For a job no worker holds, Cancel does that bookkeeping itself.
//
// Returns ErrJobNotFound when no job has that ID. Cancel is a plain stop: the
// file's health record is untouched, so a later read may queue the repair
// again.
func (s *Service) Cancel(ctx context.Context, jobID int64) error {
	s.runningMu.Lock()
	rj := s.running[jobID]
	if rj != nil {
		rj.cancelled = true
	}
	s.runningMu.Unlock()

	if rj != nil {
		rj.cancel()
		select {
		case <-rj.done:
		case <-time.After(cancelUnwindTimeout):
			return fmt.Errorf("par2repair: job %d did not stop within %s", jobID, cancelUnwindTimeout)
		case <-ctx.Done():
			return ctx.Err()
		}
		s.sweepArtifacts(ctx)
		return nil
	}

	job, err := s.repo.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return ErrJobNotFound
	}
	s.log.InfoContext(ctx, "PAR2 repair cancelled before it started",
		"file", job.FilePath, "job", jobID)
	s.failImport(ctx, job, cancelReason)
	if err := s.repo.Delete(ctx, jobID); err != nil {
		return fmt.Errorf("par2repair: delete cancelled job %d: %w", jobID, err)
	}
	s.clearProgress(jobID)
	s.sweepArtifacts(ctx)
	return nil
}

// sweepArtifacts reclaims transient repair artifacts, but only while no job is
// running: with an empty registry every file under .scratch and every .tmp-*
// in the patch tree is by definition an orphan, so no per-file ownership
// tracking is needed. Failures are logged, never fatal — the repair is already
// stopped, which is what the caller asked for.
func (s *Service) sweepArtifacts(ctx context.Context) {
	s.runningMu.Lock()
	busy := len(s.running) > 0
	s.runningMu.Unlock()
	if busy {
		return
	}
	if err := s.store.RemoveScratch(); err != nil {
		s.log.ErrorContext(ctx, "Failed to clean par2 repair scratch dir", "error", err)
	}
	if err := s.store.SweepTempFiles(); err != nil {
		s.log.ErrorContext(ctx, "Failed to sweep par2 repair temp files", "error", err)
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

```bash
go test ./internal/par2repair/ -race -v
```

Expected: PASS for the whole package, including the five new `TestServiceCancel*` tests and every pre-existing test. If a pre-existing test fails to compile because its `JobStore` fake now lacks `Get`, add the method to that fake returning `(nil, nil)`.

- [ ] **Step 7: Commit**

```bash
git add internal/par2repair/service.go internal/par2repair/service_test.go
git commit -m "feat(par2repair): cancel a queued or in-flight repair and sweep its artifacts"
```

---

### Task 4: DELETE endpoints

**Files:**
- Modify: `internal/api/par2repair_handlers.go` (add the interface and two handlers)
- Modify: `internal/api/server.go:275-276` (register the routes)
- Test: `internal/api/par2repair_handlers_test.go` (extend `par2TestApp`, append tests)

**Interfaces:**
- Consumes: `(*par2repair.Service).Cancel(ctx, jobID) error`, `par2repair.ErrJobNotFound` (Task 3); `(*database.Par2RepairRepository).List(ctx, limit)` (existing).
- Produces: `DELETE /api/par2repair/:id` and `DELETE /api/par2repair`; interface `api.Par2RepairCanceller`.

- [ ] **Step 1: Write the failing tests**

Extend `par2TestApp` in `internal/api/par2repair_handlers_test.go` to register the new routes:

```go
func par2TestApp(s *Server) *fiber.App {
	app := fiber.New()
	app.Post("/api/par2repair", s.handlePar2Repair)
	app.Get("/api/par2repair", s.handleListPar2Repair)
	app.Delete("/api/par2repair", s.handleCancelAllPar2Repair)
	app.Delete("/api/par2repair/:id", s.handleCancelPar2Repair)
	return app
}
```

Append these tests to the same file:

```go
// fakeCanceller is an enqueuer that also cancels, recording the IDs it saw.
type fakeCanceller struct {
	fakeEnqueuer
	cancelled []int64
	err       error
}

func (f *fakeCanceller) Cancel(_ context.Context, jobID int64) error {
	f.cancelled = append(f.cancelled, jobID)
	return f.err
}

func TestHandleCancelPar2Repair(t *testing.T) {
	canceller := &fakeCanceller{}
	app := par2TestApp(&Server{par2Repair: canceller})

	resp, err := app.Test(httptest.NewRequest("DELETE", "/api/par2repair/7", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(canceller.cancelled) != 1 || canceller.cancelled[0] != 7 {
		t.Fatalf("cancelled = %v, want [7]", canceller.cancelled)
	}
}

func TestHandleCancelPar2RepairErrors(t *testing.T) {
	tests := []struct {
		name       string
		server     *Server
		path       string
		wantStatus int
	}{
		{"bad id", &Server{par2Repair: &fakeCanceller{}}, "/api/par2repair/abc", 400},
		{"zero id", &Server{par2Repair: &fakeCanceller{}}, "/api/par2repair/0", 400},
		{"no service", &Server{}, "/api/par2repair/7", 503},
		{"enqueue-only service", &Server{par2Repair: &fakeEnqueuer{}}, "/api/par2repair/7", 503},
		{
			"unknown job",
			&Server{par2Repair: &fakeCanceller{err: par2repair.ErrJobNotFound}},
			"/api/par2repair/7",
			404,
		},
		{
			"will not stop",
			&Server{par2Repair: &fakeCanceller{err: errors.New("did not stop within 30s")}},
			"/api/par2repair/7",
			500,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := par2TestApp(tt.server)
			resp, err := app.Test(httptest.NewRequest("DELETE", tt.path, nil))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestHandleCancelAllPar2Repair(t *testing.T) {
	repo := newPar2RepairAPIRepo(t)
	ctx := context.Background()
	for _, p := range []string{"/movies/a.mkv", "/movies/b.mkv"} {
		if _, err := repo.Enqueue(ctx, p, ""); err != nil {
			t.Fatal(err)
		}
	}
	// Cancel deletes rows in the real service; the fake does not, so stop
	// after the first pass by reporting every job as already gone.
	canceller := &fakeCanceller{err: par2repair.ErrJobNotFound}
	app := par2TestApp(&Server{par2Repair: canceller, par2RepairRepo: repo})

	resp, err := app.Test(httptest.NewRequest("DELETE", "/api/par2repair", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(canceller.cancelled) != 2 {
		t.Fatalf("cancelled = %v, want both jobs attempted once", canceller.cancelled)
	}

	body, _ := io.ReadAll(resp.Body)
	var payload struct {
		Data struct {
			Cancelled int `json:"cancelled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal %s: %v", body, err)
	}
	// Both were already gone, so nothing counts as cancelled.
	if payload.Data.Cancelled != 0 {
		t.Fatalf("cancelled count = %d, want 0", payload.Data.Cancelled)
	}
}

func TestHandleCancelAllPar2RepairUnavailable(t *testing.T) {
	app := par2TestApp(&Server{par2Repair: &fakeCanceller{}})
	resp, err := app.Test(httptest.NewRequest("DELETE", "/api/par2repair", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d, want 503 when the repo is unset", resp.StatusCode)
	}
}
```

Add `"errors"` and `"github.com/javi11/altmount/internal/par2repair"` to that file's import block (`bytes`, `context`, `database/sql`, `encoding/json`, `io`, `net/http/httptest`, `testing`, `time`, fiber and the sqlite3 blank import are already there).

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/api/ -run 'TestHandleCancel' -v
```

Expected: FAIL to build — `s.handleCancelAllPar2Repair undefined`, `s.handleCancelPar2Repair undefined`.

- [ ] **Step 3: Implement the handlers**

Append to `internal/api/par2repair_handlers.go`:

```go
// Par2RepairCanceller stops an in-flight or queued repair and cleans its
// transient artifacts (implemented by par2repair.Service).
type Par2RepairCanceller interface {
	Cancel(ctx context.Context, jobID int64) error
}

// canceller returns the wired repair service as a canceller, or nil when PAR2
// repair is unavailable in this build/config.
func (s *Server) canceller() Par2RepairCanceller {
	c, _ := s.par2Repair.(Par2RepairCanceller)
	return c
}

// handleCancelPar2Repair handles DELETE /api/par2repair/:id: stop one queued or
// running repair and clean the artifacts it generated.
func (s *Server) handleCancelPar2Repair(c *fiber.Ctx) error {
	canceller := s.canceller()
	if canceller == nil {
		return RespondServiceUnavailable(c, "PAR2 repair is not available", "")
	}
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return RespondBadRequest(c, "Invalid job ID", c.Params("id"))
	}
	switch err := canceller.Cancel(c.Context(), id); {
	case err == nil:
		return RespondMessage(c, "PAR2 repair cancelled")
	case errors.Is(err, par2repair.ErrJobNotFound):
		return RespondNotFound(c, "PAR2 repair job", c.Params("id"))
	default:
		return RespondInternalError(c, "Failed to cancel PAR2 repair", err.Error())
	}
}

// handleCancelAllPar2Repair handles DELETE /api/par2repair: cancel every queued
// and running repair.
//
// It drains in passes rather than one shot because List caps its result at 100
// rows, so a single pass is not guaranteed to see the whole queue. A pass that
// cancels nothing ends the loop, which keeps it finite when a row cannot be
// removed.
func (s *Server) handleCancelAllPar2Repair(c *fiber.Ctx) error {
	canceller := s.canceller()
	if canceller == nil {
		return RespondServiceUnavailable(c, "PAR2 repair is not available", "")
	}
	if s.par2RepairRepo == nil {
		return RespondServiceUnavailable(c, "PAR2 repair is not available", "")
	}

	var cancelled int
	for {
		jobs, err := s.par2RepairRepo.List(c.Context(), 0)
		if err != nil {
			return RespondInternalError(c, "Failed to list PAR2 repair jobs", err.Error())
		}
		if len(jobs) == 0 {
			break
		}
		progressed := false
		for _, job := range jobs {
			err := canceller.Cancel(c.Context(), job.ID)
			switch {
			case err == nil:
				cancelled++
				progressed = true
			case errors.Is(err, par2repair.ErrJobNotFound):
				// Finished between the list and the cancel; nothing to do.
			default:
				return RespondInternalError(c, "Failed to cancel PAR2 repair", err.Error())
			}
		}
		if !progressed {
			break
		}
	}
	return RespondSuccess(c, fiber.Map{"cancelled": cancelled})
}
```

Add `"errors"` and `"strconv"` to that file's import block (`context`, `time`, fiber, `internal/database` and `internal/par2repair` are already imported).

- [ ] **Step 4: Register the routes**

In `internal/api/server.go`, directly below the existing pair at lines 275-276:

```go
	api.Delete("/par2repair", s.handleCancelAllPar2Repair)
	api.Delete("/par2repair/:id", s.handleCancelPar2Repair)
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
go test ./internal/api/ -run 'TestHandle.*Par2Repair' -v && go build ./...
```

Expected: PASS for all `Par2Repair` handler tests (new and pre-existing), and a clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/api/par2repair_handlers.go internal/api/par2repair_handlers_test.go internal/api/server.go
git commit -m "feat(api): add DELETE endpoints to cancel PAR2 repairs"
```

---

### Task 5: Frontend client and hooks

**Files:**
- Modify: `frontend/src/api/client.ts` (after `triggerPar2Repair`, ~line 463)
- Modify: `frontend/src/hooks/useApi.ts` (after `useTriggerPar2Repair`, ~line 218)

**Interfaces:**
- Consumes: `DELETE /par2repair/:id`, `DELETE /par2repair` (Task 4).
- Produces: `apiClient.cancelPar2Repair(id: number)`, `apiClient.cancelAllPar2Repairs()`, `useCancelPar2Repair()`, `useCancelAllPar2Repairs()`. Both hooks are TanStack `useMutation`s: the first takes `number`, the second no argument.

- [ ] **Step 1: Add the client methods**

In `frontend/src/api/client.ts`, immediately after `triggerPar2Repair`:

```ts
	async cancelPar2Repair(id: number) {
		return this.request<{ message: string }>(`/par2repair/${id}`, {
			method: "DELETE",
		});
	}

	async cancelAllPar2Repairs() {
		return this.request<{ cancelled: number }>("/par2repair", {
			method: "DELETE",
		});
	}
```

- [ ] **Step 2: Add the mutation hooks**

In `frontend/src/hooks/useApi.ts`, immediately after `useTriggerPar2Repair`:

```ts
export const useCancelPar2Repair = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (id: number) => apiClient.cancelPar2Repair(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["par2repair"] });
		},
	});
};

export const useCancelAllPar2Repairs = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.cancelAllPar2Repairs(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["par2repair"] });
		},
	});
};
```

- [ ] **Step 3: Verify types and lint**

```bash
cd frontend && bun run check
```

Expected: no errors. Biome may reformat; keep its output.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/hooks/useApi.ts
git commit -m "feat(frontend): add PAR2 repair cancel API client and hooks"
```

---

### Task 6: Cancel controls in the PAR2 Repairs card

Per-row cancel plus a header "Cancel all", both behind the app's existing confirm modal.

**Note on a deliberate spec deviation:** the spec described a bespoke `<dialog>` inside `Par2RepairSection`. Use the app's existing `confirmAction` from `useConfirm` (`frontend/src/contexts/ModalContext.tsx`) in `HealthPage` instead — it is the pattern every other destructive action on this page already uses (`handleCancelCheck`, `handleRepair`), so the section stays presentational and the modal stays consistent.

**Files:**
- Modify: `frontend/src/pages/HealthPage/components/Par2RepairSection.tsx`
- Modify: `frontend/src/pages/HealthPage.tsx` (imports ~line 18-25, handlers near `handlePar2Repair` ~line 372, render ~line 839)

**Interfaces:**
- Consumes: `useCancelPar2Repair()`, `useCancelAllPar2Repairs()` (Task 5); `confirmAction(title, message, options)` and `showToast({title, message, type})` (existing on `HealthPage`).
- Produces: `Par2RepairSectionProps` grows to `{ jobs: Par2RepairJob[] | undefined; onCancel: (id: number) => void; onCancelAll: () => void; isCancelling?: boolean }`.

- [ ] **Step 1: Add the Actions column and header button**

In `frontend/src/pages/HealthPage/components/Par2RepairSection.tsx`:

Extend the icon import to include `Ban`:

```tsx
import { Ban, Clock, Loader2, Wrench } from "lucide-react";
```

Replace the props interface:

```tsx
interface Par2RepairSectionProps {
	jobs: Par2RepairJob[] | undefined;
	onCancel: (id: number) => void;
	onCancelAll: () => void;
	/** Disables the controls while a cancel request is in flight. */
	isCancelling?: boolean;
}
```

Change the component signature:

```tsx
export function Par2RepairSection({
	jobs,
	onCancel,
	onCancelAll,
	isCancelling = false,
}: Par2RepairSectionProps) {
```

In the populated branch's `card-title`, after the `active` badge, add the header button:

```tsx
					<button
						type="button"
						className="btn btn-ghost btn-xs ml-auto font-normal"
						onClick={onCancelAll}
						disabled={isCancelling}
					>
						<Ban className="h-3.5 w-3.5" aria-hidden="true" />
						Cancel all
					</button>
```

Add the header cell after `<th>Updated</th>`:

```tsx
								<th>Actions</th>
```

Add the body cell after the `updated_at` `<td>`:

```tsx
										<td>
											<button
												type="button"
												className="btn btn-ghost btn-xs text-error"
												aria-label={`Cancel repair of ${fileName(job.file_path)}`}
												title="Cancel repair"
												onClick={() => onCancel(job.id)}
												disabled={isCancelling}
											>
												<Ban className="h-4 w-4" aria-hidden="true" />
											</button>
										</td>
```

Update `ReasonRow`'s column span for the new column — the call site becomes:

```tsx
									<ReasonRow job={job} columns={7} />
```

- [ ] **Step 2: Wire the handlers in HealthPage**

In `frontend/src/pages/HealthPage.tsx`, add both hooks to the existing `useApi` import list (alphabetically among the others):

```tsx
	useCancelAllPar2Repairs,
	useCancelPar2Repair,
```

Next to `const triggerPar2Repair = useTriggerPar2Repair();` (~line 104):

```tsx
	const cancelPar2Repair = useCancelPar2Repair();
	const cancelAllPar2Repairs = useCancelAllPar2Repairs();
```

Immediately after the `handlePar2Repair` callback (~line 392):

```tsx
	const handleCancelPar2Repair = useCallback(
		async (id: number) => {
			const job = par2RepairJobs?.find((candidate) => candidate.id === id);
			const name = job ? job.file_path.split("/").pop() || job.file_path : "this file";
			const confirmed = await confirmAction(
				"Cancel PAR2 Repair",
				`Cancel the repair of ${name}? Progress so far is discarded. The repair may start again the next time the file is read.`,
				{
					type: "warning",
					confirmText: "Cancel Repair",
					confirmButtonClass: "btn-warning",
				},
			);
			if (!confirmed) {
				return;
			}
			try {
				await cancelPar2Repair.mutateAsync(id);
				showToast({
					title: "PAR2 Repair Cancelled",
					message: "The repair stopped and its temporary files were cleaned up.",
					type: "success",
				});
			} catch (err: unknown) {
				const error = err as { message?: string };
				console.error("Failed to cancel PAR2 repair:", err);
				showToast({
					title: "Cancel Failed",
					message: error.message || "Could not cancel the repair",
					type: "error",
				});
			}
		},
		[par2RepairJobs, confirmAction, cancelPar2Repair, showToast],
	);

	const handleCancelAllPar2Repairs = useCallback(async () => {
		const count = par2RepairJobs?.length ?? 0;
		const confirmed = await confirmAction(
			"Cancel All PAR2 Repairs",
			`Cancel all ${count} queued and running repairs? Progress so far is discarded.`,
			{
				type: "warning",
				confirmText: "Cancel All",
				confirmButtonClass: "btn-warning",
			},
		);
		if (!confirmed) {
			return;
		}
		try {
			const result = await cancelAllPar2Repairs.mutateAsync();
			showToast({
				title: "PAR2 Repairs Cancelled",
				message: `Stopped ${result.cancelled} repair${result.cancelled === 1 ? "" : "s"}.`,
				type: "success",
			});
		} catch (err: unknown) {
			const error = err as { message?: string };
			console.error("Failed to cancel PAR2 repairs:", err);
			showToast({
				title: "Cancel Failed",
				message: error.message || "Could not cancel the repairs",
				type: "error",
			});
		}
	}, [par2RepairJobs, confirmAction, cancelAllPar2Repairs, showToast]);
```

Replace the render site (~line 839):

```tsx
							<Par2RepairSection
								jobs={par2RepairJobs}
								onCancel={handleCancelPar2Repair}
								onCancelAll={handleCancelAllPar2Repairs}
								isCancelling={cancelPar2Repair.isPending || cancelAllPar2Repairs.isPending}
							/>
```

- [ ] **Step 3: Verify types, lint and build**

```bash
cd frontend && bun run check && bun run build
```

Expected: no type errors, no lint errors, successful Vite build.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/HealthPage.tsx frontend/src/pages/HealthPage/components/Par2RepairSection.tsx
git commit -m "feat(frontend): cancel PAR2 repairs from the Health page"
```

---

### Task 7: Full verification

**Files:** none modified (verification only; fix forward if something fails).

**Interfaces:**
- Consumes: everything from Tasks 1-6.
- Produces: a green build.

- [ ] **Step 1: Run the full Go test suite with the race detector**

```bash
go test ./... -race
```

Expected: PASS. The cancel path spans goroutines, so `-race` is the point of this step, not `go test ./...` alone.

- [ ] **Step 2: Run go vet**

```bash
go vet ./...
```

Expected: no output.

- [ ] **Step 3: Run the full project build**

```bash
make
```

Expected: backend and frontend both build, all checks pass. This is the gate CLAUDE.md requires before commit.

- [ ] **Step 4: Manually verify the round trip**

Start AltMount, queue a repair from a file's actions menu on the Health page, then press the row's cancel button and confirm. Check:
- the row disappears from the PAR2 Repairs card,
- the log carries `PAR2 repair cancelled` with the file and job ID,
- `<patch-dir>/.scratch` is gone from disk,
- no `.patch` files were deleted.

- [ ] **Step 5: Commit any fixes**

```bash
git add -A
git commit -m "fix(par2repair): address verification findings"
```

Skip this step if steps 1-4 were clean and nothing needed changing.
