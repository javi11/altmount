# Seek Ramp and Buffered Forward-Skip Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop a fresh reader from firing its whole prefetch window before the caller has shown streaming intent, and stop a forward jump from draining articles through the pipeline when a new reader would be cheaper (spec phase 4, defect D5, plus the B7 finding from phase 0).

**Architecture:** `UsenetReader` grows its window as `rampBase << (2 × consumed)` up to `maxPrefetch`, where `consumed` is segments fully read since the reader was created (every seek creates a reader, so the anchor is creation). A range hint of at least 128 MiB (WebDAV open-ended or large ranges) skips the ramp. `MetadataVirtualFile` only drains a forward gap through the shared reader when the gap fits inside what that reader has already scheduled (`BufferedAhead`); otherwise it reopens at the target.

**Spec:** `docs/superpowers/specs/2026-09-04-streaming-demand-shaping-design.md` (Phase 4)

## Global Constraints

- No competitor names in code, comments, commits, PRs.
- Conventional Commits; branch `feat/streaming-seek-ramp` from `feat/streaming-shared-readahead-budget`.
- `make bench-compare BASE=baseline-main`: no regressions; B2 throughput must hold (the ramp completes within the first three articles).

---

### Task 1: B9 forward-skip scenario (pre-sample before the change)

**Files:** `internal/nzbfilesystem/stream_bench_test.go`

Add `BenchmarkStreamForwardSkip` (premium profile): open, read 2 MB from 0, then read 2 MB at +100 MB, then 2 MB at +2 MB from there. Metrics: `jump_read_ms` (latency of the +100 MB read, gated, tolerance 0.10), `articles` (bodies fetched by the whole scenario, gated), `open_articles` (bodies fetched before the jump, info). Run it once on this branch before Tasks 2-4 and keep the numbers for the PR.

- [ ] Add, `gofmt`, commit `test(bench): forward-skip scenario`, sample: `go test ./internal/nzbfilesystem/ -run '^$' -bench BenchmarkStreamForwardSkip -benchtime 1x`.

### Task 2: Ramp in `UsenetReader`

**Files:** `internal/usenet/usenet_reader.go`, `internal/usenet/usenet_reader_ramp_test.go`

**Interfaces:**
```go
const (
	rampBaseSegments     = 2
	streamingIntentBytes = 128 << 20
)
// WithRangeHint tells the reader how many bytes the caller asked for. At or
// above streamingIntentBytes the read-ahead window opens fully at once;
// below it (or unknown, 0) the window ramps from rampBaseSegments,
// quadrupling per consumed segment, so a probe that reads a couple of MB
// and leaves does not drag a whole window behind it.
func WithRangeHint(bytes int64) ReaderOption
// BufferedAhead reports bytes scheduled for fetch beyond what the caller has
// read: the distance a forward skip can cover without a new reader.
func (b *UsenetReader) BufferedAhead() int64
```
Window: `func (b *UsenetReader) windowFor(consumed int) int` returns `maxPrefetch` when `!b.ramp`, else `min(maxPrefetch, rampBaseSegments << (2*consumed))` guarding overflow (`consumed >= 16` → `maxPrefetch`). In `downloadManager` replace `if ahead >= b.maxPrefetch` with `if ahead >= b.windowFor(currentRead)`. Ramp applies to streaming readers only (`priority == true`). `BufferedAhead` = `scheduledBytes - totalBytesRead`, where `scheduledBytes` accumulates `seg.End-seg.Start+1` under `b.mu` when a segment is scheduled.

- [ ] Tests: (a) fresh reader, 40 segments, segment 0 gated: after a settle, `BodyPriorityCalls <= rampBaseSegments`; release and read all: `MaxInFlight > rampBaseSegments` eventually and every byte correct. (b) `WithRangeHint(200 << 20)`: with 30 ms latency per segment, `MaxInFlight == maxPrefetch` within the read. (c) import reader (`WithImportProfile`) is unaffected: max in flight equals maxPrefetch on the first segments. (d) `BufferedAhead` after `Start()` with segment 0 gated equals the scheduled segments' bytes and drops as bytes are read.
- [ ] Implement; `go test -race ./internal/usenet/`; commit `feat(usenet): ramp read-ahead after open or seek; report buffered bytes`.

### Task 3: Range hint and buffered forward-skip in `MetadataVirtualFile`

**Files:** `internal/nzbfilesystem/metadata_remote_file.go` (`createUsenetReader`, the forward-skip block around line 1265, `bufOffReader` sibling assertion), `internal/nzbfilesystem/forward_skip_test.go`

- Hint: in `createUsenetReader(ctx, start, end)`, `hint := int64(0)`; if the context carries `utils.RangeKey` (WebDAV) then `hint = end - start + 1` with `end == -1` meaning `FileSize - start`. FUSE passes no range and ramps (its read-ahead buffer promotes after three sequential reads anyway).
- Forward skip: keep `forwardSkipLimit` as the outer bound, but when the reader exposes `BufferedAhead()` and `gap > buffered`, do not drain: `closeCurrentReader()`, `mvf.position = off`, `mvf.readAtSharedNext = off`, then continue into the shared path (`ensureReader` opens at the cursor). Add `bufAheadReader interface{ BufferedAhead() int64 }` next to `bufOffReader`.

- [ ] Tests: (a) 200 segments × 64 KiB, `maxPrefetch` 4, WebDAV-less ctx: `ReadAt(64 KiB @0)`, then `ReadAt(64 KiB @ 40 segments)`: `BodyPriorityCalls <= 12` (not ≥ 44) and both reads return exact bytes. (b) small skip inside the buffered window (`maxPrefetch` 10, jump 2 segments): the shared reader pointer is unchanged and bytes are exact. (c) ctx with `utils.RangeKey = "bytes=0-"` on a 300 MB synthetic file: first read schedules more than `rampBaseSegments` fetches (hint skips the ramp).
- [ ] Implement; `go test -race ./internal/nzbfilesystem/`; commit `feat(nzbfilesystem): range-aware ramp and buffered forward-skip`.

### Task 4: Benchmark, live A/B, PR

- [ ] `make bench-stream && make bench-compare BASE=baseline-main`. Expect B9 `articles` and `jump_read_ms` well below the Task 1 sample, B1 `articles` (info) down to a handful on both profiles, B2 unchanged.
- [ ] `bench-live.sh seek-ramp`.
- [ ] Push; `gh pr create --base feat/streaming-shared-readahead-budget`.
