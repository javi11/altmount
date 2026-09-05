# Stream Body Depth and Abandoned-Drain Abort Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop a closed handle's read-ahead from draining through the wire after a seek or episode change, and bound how many stream bodies queue on one connection ahead of a demand read (spec phase 7, defect D9, plus the tail effect measured in phases 4 and 5).

**Architecture (nntppool, released as v4.22.0):**
1. **Abandoned-drain abort.** In `readerLoop`, a body whose request context is done is currently drained to `io.Discard`. When the bytes still to come exceed `Provider.AbortDrainBytes` (default 1 MiB; 0 keeps the old behaviour), the reader closes the connection instead. Pending neighbours fail with `ErrConnectionDied` and the existing connection-death retry re-dispatches the ones still wanted; the slot reconnects (TLS resumption from phase 1 makes that one round trip). Articles under the threshold keep draining, since a reconnect costs more than 94 ms of 750 KB. Remaining bytes come from the yEnc part size the decoder already parsed minus bytes decoded; when the size is unknown, drain as before.
2. **Stream body depth.** `Provider.StreamInflight` (default `min(Inflight, 4)`) caps priority-lane bodies in flight per connection through a second semaphore taken alongside `bodySem`, so a demand read never queues behind more than three stream bodies on its connection. Normal-lane (import) bodies keep `Inflight`.

**Architecture (altmount):** `ToNNTPProvider` sets `StreamInflight` from a new `providers[].stream_inflight_requests` (default 4) and leaves `AbortDrainBytes` at the library default. New bench scenario B11 measures the seek pattern the abort targets.

**Spec:** `docs/superpowers/specs/2026-09-04-streaming-demand-shaping-design.md` (Phase 7)

## Global Constraints

- No competitor names in code, comments, commits, PRs.
- Conventional Commits; branch `feat/streaming-body-depth` from `feat/streaming-memory-segcache`; nntppool branch `feat/abort-drain-stream-depth`.
- `make bench-compare BASE=baseline-main`: no regressions; B1 slow-4m cold open and B11 must improve; B2/B6 steady state must hold.
- No `replace` in `go.mod` when the PR is marked ready.

---

### Task 1: B11 seek scenario, pre-sampled

`BenchmarkStreamSeekAndResume` (both profiles): open, read 8 MB at 0, then five times: jump forward 500 MB, read 8 MB. Metrics: `seek_read_ms` mean of the five (gated, tolerance 0.10), `bytes_fetched_mb` for the whole scenario (gated), expected today to include the abandoned windows.

- [ ] Add, commit `test(bench): seek-and-resume scenario`, sample once on this branch.

### Task 2: nntppool abandoned-drain abort

**Files:** `~/mio/nntppool/nntp.go` (`Provider.AbortDrainBytes`, `NNTPConnection.abortDrainBytes`, readerLoop drain callback), `~/mio/nntppool/abort_drain_test.go`.

In the drain callback, once `deliver` has flipped to false: `remaining := decoder.expectedRemaining()` (part size minus bytes decoded, -1 when unknown); if `remaining > c.abortDrainBytes && c.abortDrainBytes > 0`, return a sentinel deadline in the past so `feedUntilDone` returns a timeout, and mark `c.abortDrain = true` so the timeout handling closes the connection with `connDiedErr` rather than treating it as a stall. Verify how `readerLoop` turns read errors into connection death (`nntp.go:1499-1518`) and reuse that path.

- [ ] Test with a `net.Pipe` server (pattern: `TestClient_BodyPriority`): serve a 4 MB yEnc body slowly (writer sleeps per 64 KB); cancel the request context after the first chunk; assert the server sees its connection closed within 500 ms and that a following `Body` on the client succeeds (new connection). Second test: 200 KB body, cancel, assert the connection is *not* closed (drained) and the next request reuses it (server connection count stays 1).
- [ ] Implement; `go test ./ -run 'AbortDrain|BodyPriority|Slow'`; commit `feat(conn): abort draining an abandoned body when more than AbortDrainBytes remain`.

### Task 3: nntppool stream body depth

**Files:** `~/mio/nntppool/nntp.go` (`Provider.StreamInflight`, `NNTPConnection.prioBodySem`, writer acquire/release paths), `~/mio/nntppool/stream_depth_test.go`.

Where the writer takes `bodySem` for a body-bearing request (`nntp.go:~992`), also take `prioBodySem` when `req.priority`; release both where `bodySem` is released. `Request` must carry `priority bool` (set in `tryGroupTimeout` from the lane). Default `StreamInflight = min(Inflight, 4)`; `StreamInflight >= Inflight` disables the extra bound.

- [ ] Test: single connection, `Inflight: 10, StreamInflight: 2`, server holds bodies until released; issue 4 `BodyPriority`; assert the server has received only 2 BODY commands until one completes. Control: 4 `Body` (normal) → all 4 received.
- [ ] Implement; full nntppool suite; commit `feat(conn): cap priority-lane bodies per connection with StreamInflight`; tag `v4.22.0` after altmount validation (Task 5).

### Task 4: altmount wiring

**Files:** `internal/config/manager.go` (`ProviderConfig.StreamInflightRequests`, `ToNNTPProvider`), `internal/config/provider_nntp_test.go`, `config.sample.yaml`, `frontend/src/types/config.ts` (optional field), bench harness (`benchProfile.StreamInflight`).

- [ ] Tests: default 4; explicit value passes through; capped at `inflight_requests`.
- [ ] Commit `feat(pool): stream_inflight_requests per provider`.

### Task 5: Benchmark, live, release, PR

- [ ] `go mod edit -replace` to `~/mio/nntppool`, `make bench-stream`, compare. Expect: B11 bytes fetched down sharply on slow-4m and seek read time down; B1 slow-4m cold open well under 1 s (the next open no longer queues behind the previous handle's tail); B2/B6 steady state within tolerance. If B2 steady state on the premium profile drops with `StreamInflight 4`, raise the default to 6 and re-measure before accepting.
- [ ] Live: `bench-live.sh body-depth`; additionally seek-storm one 2160p file with ten ranged reads 1 GB apart and compare provider bytes delivered before/after (dashboard metrics or the pool stats endpoint).
- [ ] Release nntppool v4.22.0 (fast-forward `main`, tag, push), drop the replace, `go get`, commit `chore(deps): nntppool v4.22.0`, push, `gh pr create --base feat/streaming-memory-segcache`.
