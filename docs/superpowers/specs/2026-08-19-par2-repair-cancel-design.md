# PAR2 Repair Cancellation — Design

Date: 2026-08-19
Scope: `internal/par2repair`, `internal/api`, `internal/database`, `frontend/src`

## Problem

A PAR2 repair streams an entire release once, so a job runs for minutes to
hours. Today there is no way to stop one: the only controls are enqueue
(stream miss, health check, or the file's actions menu) and waiting. A user who
queued a repair by mistake, or who wants the connections back, has to restart
AltMount — and a restart leaves the job row `running` until `ResetRunning`
reclaims it at boot.

## Goals

- Stop a queued or in-flight repair from the Health page.
- Leave no transient artifacts behind: solver arenas under the patch store's
  `.scratch` directory and `.tmp-*` files from interrupted `PatchStore.Put`
  calls.
- Release an import parked in `waiting_repair` by a cancelled NZB-mode job, so
  it cannot wait forever.

## Non-goals

- No suppression of future repairs. Cancel stops **this attempt only**; the
  file's health record and metadata are left as they are, so the next stream
  read or health check may legitimately queue the repair again.
- No purging of stored `.patch` files. Patches are valid repaired bytes already
  served on the read path, and they are keyed by message-ID hash with no
  job→patch mapping, so a cancelled job cannot identify "its" patches.
- No cross-instance cancellation. AltMount runs one instance per patch root;
  an in-process registry is sufficient.

## Key facts that shape the design

- `emitPatches` runs only after a successful solve, so a job cancelled
  mid-sweep has written **zero** patches. Cleanup is about scratch/tmp files,
  not patches.
- Both the payload arena and the per-attempt arena are `defer`-closed in
  `RunJob` (`job.go:150`, `job.go:208`), and `arena.Close` unmaps and removes
  its scratch file. Therefore: **waiting for the job goroutine to unwind is
  what makes cleanup deterministic.** Cancel must not return before that.
- `runNext` currently treats `ctx.Err() != nil` as shutdown and leaves the row
  `running` for boot-time recovery. A user cancel must be distinguishable from
  shutdown, or cancelled rows would linger as `running`.

## Chosen approach

**In-process cancel registry.** `Service` tracks the jobs it is running with a
per-job cancel function and a done channel. `Cancel` cancels the job's context,
waits for the worker to unwind (running the arena `defer`s), and the worker
performs the terminal bookkeeping so that logic has a single owner.

Rejected alternatives:

- *DB `cancel_requested` flag polled by the sweep loop.* Buys cross-instance
  cancellation AltMount does not need, at the cost of a migration, DB reads in
  the hot loop, and cancel latency of up to one poll interval.
- *Fire-and-forget context cancel.* The HTTP response would claim cleanup that
  has not happened, and `handleOutcome` can race and `MarkRetry` the row back
  into existence.

## Service layer (`internal/par2repair/service.go`)

```go
type runningJob struct {
    cancel    context.CancelFunc
    done      chan struct{} // closed when the worker unwinds
    cancelled bool          // set by Cancel, read by the worker
}
```

`Service` gains `runningMu sync.Mutex` and `running map[int64]*runningJob`.

- `runNext` derives `jobCtx, cancel := context.WithCancel(ctx)`, registers the
  job before calling `s.execute(jobCtx, job)`, and on return deregisters it and
  closes `done`. It reads the `cancelled` flag while deregistering and passes
  it to `handleOutcome`.
- `Cancel(ctx context.Context, id int64) error`:
  - **Running:** set `cancelled`, call `cancel()`, wait on `done` with a 30s
    bound. On timeout return an error stating the job is still unwinding (the
    row is left alone; the caller may retry).
  - **Pending / not running:** `JobStore.Get(ctx, id)`; nil → `ErrJobNotFound`.
    Fail its parked import, then delete the row.
  - Finally call `sweepScratch()`.
- `handleOutcome` gains a cancelled branch **ahead of** the retry branch:
  log at info, no health-record write, `failImport(ctx, job, "repair cancelled
  by user")` for NZB-mode jobs, `deleteJob`, `clearProgress`. This is what keeps
  a cancelled row from being marked for retry or left `running`.
- `sweepScratch()` runs only while the registry is empty. With nothing running,
  every file under `.scratch` and every stale `.tmp-*` in the patch tree is by
  definition an orphan, so no per-file ownership tracking is needed.
- `var ErrJobNotFound = errors.New("par2repair: job not found")` so the API can
  map it to 404.

### PatchStore

Two new methods, both idempotent and safe on a missing root:

- `RemoveScratch() error` — `os.RemoveAll(p.ScratchDir())`.
- `SweepTempFiles() error` — walk the root, remove files whose base name starts
  with `.tmp-`, skip `.patch` files and directories.

### Repository / JobStore

`JobStore` gains `Get(ctx context.Context, id int64) (*database.Par2RepairJob, error)`,
implemented by `Par2RepairRepository` as a single-row `SELECT` reusing the
existing row scanner. Returns `(nil, nil)` when the id does not exist. No
migration: cancellation needs no new persisted state.

## API layer (`internal/api/par2repair_handlers.go`)

```go
// Par2RepairCanceller stops an in-flight or queued repair and cleans its
// transient artifacts (implemented by par2repair.Service).
type Par2RepairCanceller interface {
    Cancel(ctx context.Context, jobID int64) error
}
```

Routes registered alongside the existing pair in `server.go`:

| Method | Path | Handler | Behaviour |
| --- | --- | --- | --- |
| DELETE | `/api/par2repair/:id` | `handleCancelPar2Repair` | 400 unparseable id, 503 when `s.par2Repair` is nil or not a `Par2RepairCanceller`, 404 on `ErrJobNotFound`, 500 when the job will not unwind, else `RespondMessage(c, "PAR2 repair cancelled")`. |
| DELETE | `/api/par2repair` | `handleCancelAllPar2Repair` | Drains the queue: repeatedly `s.par2RepairRepo.List(ctx, 0)` and `Cancel` each id, tolerating `ErrJobNotFound` (a job may finish between list and cancel), stopping when a pass lists nothing or cancels nothing. Returns `RespondSuccess(c, fiber.Map{"cancelled": n})`. |

Cancel-all cancels sequentially: each `Cancel` waits for its job to unwind, and
concurrency is bounded by `MaxConcurrentJobs`, not by queue length. It loops
rather than making a single pass because `List` caps its result at 100 rows
(`par2RepairListDefaultLimit`), so one pass is not guaranteed to see the whole
queue. The "cancelled nothing this pass" exit keeps the loop finite if a row
cannot be removed.

## Frontend

- `api/client.ts`: `cancelPar2Repair(id: number)` → `DELETE /par2repair/${id}`;
  `cancelAllPar2Repairs()` → `DELETE /par2repair`.
- `hooks/useApi.ts`: `useCancelPar2Repair()` and `useCancelAllPar2Repairs()`,
  both invalidating `["par2repair"]` on success, mirroring
  `useTriggerPar2Repair`.
- `Par2RepairSection.tsx`:
  - New **Actions** column: `btn btn-ghost btn-xs` with a `Ban` icon and
    `aria-label="Cancel repair"`, disabled while its mutation is pending. The
    table goes 6 → 7 columns, so `ReasonRow`'s `columns` prop becomes 7.
  - Header **Cancel all** button (`btn btn-ghost btn-xs`), rendered only when
    jobs exist.
  - Confirmation via a DaisyUI `<dialog className="modal">` driven by local
    state `confirm: {kind: "one", job: Par2RepairJob} | {kind: "all"} | null`.
    Single-job copy: "Cancel repair of `<filename>`? Progress so far is
    discarded. The repair may start again the next time the file is read."
    All: "Cancel all N queued and running repairs?"
  - Props grow to `{ jobs, onCancel, onCancelAll }`; mutations are wired in
    `HealthPage.tsx` next to `handlePar2Repair`, reporting through the existing
    `showToast`.

## Error handling

- Every cleanup failure (`RemoveScratch`, `SweepTempFiles`, `deleteJob`,
  `failImport`) is logged with `slog.*Context` and never fatal — the repair is
  already stopped, which is what the user asked for.
- A `Cancel` that times out waiting for unwind returns an error and leaves the
  row untouched, so a retry is safe and idempotent.
- `Cancel` on an id that finished a moment earlier returns `ErrJobNotFound`
  → 404; cancel-all swallows it.

## Testing

`internal/par2repair/service_test.go`:

- Cancel a running job (fake `execute` blocking on ctx): row deleted, no
  `MarkRetry`, progress cleared, and `Cancel` returns only after the job
  goroutine has exited.
- Cancel a running NZB-mode job: `FailWaitingRepair` called with the cancel
  reason.
- Cancel a pending job: row deleted directly, no registry involvement.
- Cancel an unknown id: `ErrJobNotFound`.
- Cancel does not write a health record (plain-stop semantics).

`internal/par2repair/patchstore_test.go`: `RemoveScratch` and `SweepTempFiles`
remove exactly the transient files and leave `.patch` files intact.

`internal/api/par2repair_handlers_test.go`: 400 bad id, 404 unknown, 503 no
service, 200 happy path, cancel-all count.

`internal/database/par2repair_repository_test.go`: `Get` returns the row, and
`(nil, nil)` for a missing id.

Frontend has no test harness for this section; verification is `bun run check`
followed by `make`.
