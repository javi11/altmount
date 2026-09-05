# In-Flight Article Dedup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two readers that want the same article at the same time download it once (spec phase 5, defect D4): two handles on one file, or a shared and an ephemeral reader on one handle.

**Architecture:** The watermark buffer moves from `segment` to a shared, article-wide `articleBuf` keyed by message-ID in a process-wide sharded `flightMap` inside `internal/usenet`. The first reader to need an article becomes its leader and streams into the `articleBuf`; later readers join and read the same buffer through their own `[Start, End]` trim. If a leader's reader is closed mid-article, the next waiting follower claims leadership and continues into a fresh attempt past the published watermark. Entries leave the map when the article is complete and no segment references it. Supersede is not needed: every stream fetch already rides the priority lane.

**Spec:** `docs/superpowers/specs/2026-09-04-streaming-demand-shaping-design.md` (Phase 5)

## Global Constraints

- No competitor names in code, comments, commits, PRs.
- Conventional Commits; branch `feat/streaming-flight-dedup` from `feat/streaming-seek-ramp`.
- `make bench-compare BASE=baseline-main`: no regressions; B5 `duplicate_bodies` must drop to 0.

---

### Task 1: `articleBuf` and `flightMap`

**Files:** create `internal/usenet/flight.go`, `internal/usenet/flight_test.go`; modify `internal/usenet/segment.go`.

**Interfaces (package-private):**
```go
// articleBuf is one article's decoded bytes as they arrive, shared by every
// segment (reader) that wants it.
type articleBuf struct{ /* mu, buf, ready, done, err, attempt, notify, refs, leading bool */ }
func (a *articleBuf) attemptWriter() *articleWriter   // same watermark rules as before
func (a *articleBuf) finish(w *articleWriter)
func (a *articleBuf) setData(data []byte)
func (a *articleBuf) setError(err error)
func (a *articleBuf) published() int64
func (a *articleBuf) waitDone(ctx context.Context) error // blocks until done/error
// claimLead returns true for exactly one caller while no leader is active;
// a leader whose attempt ended without finishing releases leadership so a
// follower can take over.
func (a *articleBuf) claimLead() bool
func (a *articleBuf) releaseLead()

type flightMap struct{ shards [16]struct{ mu sync.Mutex; m map[string]*articleBuf } }
var flights flightMap
// acquire returns the article's buffer, creating it on first use, and adds a
// reference; release drops the reference and deletes the entry once it is
// done and unreferenced (or errored and unreferenced).
func (f *flightMap) acquire(id string, size int64) *articleBuf
func (f *flightMap) release(id string, a *articleBuf)
func (f *flightMap) len() int // tests
```
`segment` keeps `Id/Start/End/SegmentSize/groups/loaderIdx` and gains `art *articleBuf` (acquired lazily in `attach()` by the download goroutine or by `SetData`/`SetError` callers), `released`, `reader`. `segmentReader` reads from `s.art`. `SetData`/`SetError` delegate to the article (a zero-filled hole or a patch is per-reader policy, but the bytes are identical for every reader of that article, so sharing them is correct; a `SetError` from one reader's policy would leak to others, so `SetError` from the hole path must not be shared — see Task 2). `Release()` drops the reference; the article itself is not torn down while others hold it. `Release()` on a segment whose article is still downloading must unblock only this segment's reader: `segmentReader` checks `s.released` first.

- [ ] Tests: two segments on one ID see the same bytes progressively; release of one does not affect the other; entry removed once done and both released; `claimLead` exclusive and re-claimable after `releaseLead`; `waitDone` unblocks on finish, error, and ctx cancel.
- [ ] Implement; `go test -race ./internal/usenet/` (existing segment tests must still pass unchanged in behaviour).
- [ ] Commit `feat(usenet): article buffers shared across readers through a flight map`.

### Task 2: Leader/follower fetch

**Files:** `internal/usenet/usenet_reader.go` (`downloadSegmentWithRetry`, goroutine tail), `internal/usenet/usenet_reader_flight_test.go`.

`downloadSegmentWithRetry(ctx, seg)` for streaming readers:
1. `art := seg.attach()`; loop:
   - if `art.claimLead()`: run the existing retry loop with `w := art.attemptWriter()` and `BodyStreamPriority`; on success `art.finish(w)`, cache `Put`, return; on `ErrArticleNotFound` `art.setError(err)` (shared: the article really is gone) and return; on any other final error, if the reader's own ctx is done, `art.releaseLead()` and return ctx.Err() (a follower may take over); otherwise `art.setError(err)` and return.
   - else: `err := art.waitDone(ctx)`; if `err == nil` return `(nil, nil)` (bytes already in the shared buffer); if the article ended in error return it; if `waitDone` returned because the leader gave up (buffer not done, no leader) loop to claim.
2. The goroutine tail: streaming readers no longer call `SetData` on success (unchanged from phase 2); the hole path keeps `SetData(zeros)` per segment, which must **not** write into the shared article: give `segment` a private override (`s.override []byte`) used by `SetData` when `s.art` is already shared and not done; simpler: `SetData` on a segment always writes a private buffer (`s.local`) that `segmentReader` prefers over `s.art`. Cache hits and patches also use `SetData` and are private.

- [ ] Tests: (a) two readers over the same 20-segment file with 20 ms latency: `BodyStreamPriorityCalls == 20`, both read exact bytes; (b) leader reader interrupted after the first chunk (gate + `Interrupt()`): the follower completes with exact bytes and total calls == 2; (c) a hole on one reader (OnHole → pad) does not pad the other reader (its `OnHole` returns fail and it gets `ErrArticleNotFound`); (d) import readers bypass the flight map (buffered `Body`, counts unchanged).
- [ ] Implement; `go test -race ./internal/usenet/ ./internal/nzbfilesystem/`.
- [ ] Commit `feat(usenet): readers join an in-flight article instead of fetching it twice`.

### Task 3: Benchmark, live A/B, PR

- [ ] `make bench-stream && make bench-compare BASE=baseline-main`: B5 `duplicate_bodies` → 0; B2/B4 unchanged.
- [ ] Live: run two concurrent `curl` reads of the same file from the dev server and compare provider bytes in the dashboard metrics (or `bench-live.sh flight-dedup` for the standard table).
- [ ] Push; `gh pr create --base feat/streaming-seek-ramp`.
