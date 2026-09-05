# Shared Read-Ahead Budget Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bound speculative (read-ahead) fetches pool-wide so N handles cannot each open a full window, while demand fetches always proceed (spec phase 3, defects D2 and D3 in part).

**Architecture:** `pool.SpeculativeBudget` is a non-blocking counting semaphore owned by `pool.Manager`, sized from total provider connections. `UsenetReader.downloadManager` classifies each segment as demand (within `demandDepth` of the read position; fetched unconditionally) or speculative (must `TryAcquire` a slot; skipped this round otherwise and re-checked when the reader advances or a fetch completes).

**Deviation from spec, and why:** the spec routed speculative fetches to the normal lane. That would let a saturating import take bandwidth from a stream's read-ahead, which today it cannot, so single-stream throughput under import could fall. All stream fetches stay on the priority lane; only the budget is added. B6 gains a gated `stream_mbps` metric to keep this measured. Cross-stream demand-over-read-ahead ordering needs a third lane in nntppool and is deferred to the body-depth phase.

**Spec:** `docs/superpowers/specs/2026-09-04-streaming-demand-shaping-design.md` (Phase 3)

## Global Constraints

- No competitor names in code, comments, commits, PRs.
- Conventional Commits; branch `feat/streaming-shared-readahead-budget` from `feat/streaming-progressive-delivery`.
- `make bench-compare BASE=baseline-main`: no regressions; B4 min-stream and B6 stream throughput must hold or improve.

---

### Task 1: `pool.SpeculativeBudget`

**Files:** create `internal/pool/specbudget.go`, `internal/pool/specbudget_test.go`; modify `internal/pool/manager.go` (interface + `manager` field), `internal/pool/config.go` (wire capacity), and the nine fake managers listed by `grep -rn "SetStreamHeadroom(" --include='*_test.go' internal | grep "func ("` plus `internal/nzbfilesystem/stream_bench_harness_test.go` if it fakes the interface (it uses the real manager; no change).

**Interfaces:**
```go
// SpeculativeBudget bounds read-ahead fetches across every stream on the pool.
type SpeculativeBudget struct{ /* mu, inFlight, capacity */ }
func NewSpeculativeBudget() *SpeculativeBudget
// SetCapacity derives the slot count from total provider connections:
// three bodies per connection, minus one so a demand read never finds every
// slot taken by read-ahead. Zero or negative disables the budget (unlimited).
func (b *SpeculativeBudget) SetCapacity(totalConns int)
func (b *SpeculativeBudget) Capacity() int
func (b *SpeculativeBudget) InFlight() int
// TryAcquire never blocks. ok=false means the caller should skip this fetch
// for now. release must be called exactly once when ok is true.
func (b *SpeculativeBudget) TryAcquire() (release func(), ok bool)
```
Manager interface gains `SpeculativeBudget() *SpeculativeBudget`; fakes return `nil`. `RegisterConfigHandlers` calls `poolManager.SpeculativeBudget().SetCapacity(total)` wherever it sets the import capacity.

- [ ] Tests: capacity formula (`SetCapacity(50)` → 149; `SetCapacity(0)` → unlimited: 1000 acquires all ok, InFlight stays 0); TryAcquire returns false at capacity and true again after a release; release is idempotent (double call does not free two slots).
- [ ] Implement; `go build ./... && go vet ./...`; add the nil-returning method to every fake until the build is green.
- [ ] Commit: `feat(pool): pool-wide speculative fetch budget`.

### Task 2: Reader tiering

**Files:** modify `internal/usenet/usenet_reader.go` (struct field, option, `downloadManager`), create `internal/usenet/usenet_reader_budget_test.go`.

**Interfaces:**
```go
// SpecBudget grants non-blocking read-ahead slots; nil means unlimited.
type SpecBudget interface{ TryAcquire() (release func(), ok bool) }
func WithSpeculativeBudget(b SpecBudget) ReaderOption
const demandDepth = 2 // segments at and just past the read position fetch unconditionally
```
`downloadManager` loop, after the `maxPrefetch` check: `ahead := b.nextToDownload - currentRead`; if `ahead >= demandDepth && b.specBudget != nil` then `release, ok := b.specBudget.TryAcquire()`; if `!ok` → `cond.Wait()` and `continue` (same shape as the maxPrefetch wait). The goroutine defers `release()` (no-op for demand). A nil interface value and a typed nil `*pool.SpeculativeBudget` both mean unlimited: guard with `if b.specBudget != nil` and make `(*SpeculativeBudget)(nil).TryAcquire()` return `(noop, true)`.

- [ ] Tests (fake budget with capacity N recording max in-flight): (a) with capacity 3 and a 20-segment file whose fetches are gated, speculative in-flight never exceeds 3 while demand fetches (segment 0 and 1) still start; (b) with the budget exhausted by another holder, the reader still completes a full sequential read (demand path never blocks on the budget); (c) `WithSpeculativeBudget(nil)` behaves exactly as before (`BodyPriorityCalls == nSegs`, max in-flight == maxPrefetch).
- [ ] Implement; `go test -race ./internal/usenet/`.
- [ ] Commit: `feat(usenet): demand fetches bypass the shared read-ahead budget`.

### Task 3: Wire through MetadataVirtualFile

**Files:** `internal/nzbfilesystem/metadata_remote_file.go:2047` and `:2176` pass `usenet.WithSpeculativeBudget(mvf.poolManager.SpeculativeBudget())`.

- [ ] `go test -race ./internal/nzbfilesystem/`; commit `feat(nzbfilesystem): readers share the pool-wide read-ahead budget`.

### Task 4: B6 stream throughput metric, benchmark, PR

- [ ] In `stream_bench_test.go` B6, record `{Name: "stream_mbps", Unit: "MB/s", Value: mbps(off, elapsed), HigherIsBetter: true, Tolerance: 0.10}`. Before implementing Task 1-3 on this branch, run `go test ./internal/nzbfilesystem/ -run '^$' -bench BenchmarkStreamUnderContention -benchtime 1x` once and keep the number for the PR table (the committed baseline predates the metric).
- [ ] `make bench-stream && make bench-compare BASE=baseline-main`; expect B4 `min_stream_mbps` up, B5/B2 unchanged, B6 `stream_mbps` within tolerance of the pre-change sample.
- [ ] Live A/B `bench-live.sh shared-budget`.
- [ ] Push; `gh pr create --base feat/streaming-progressive-delivery`.
