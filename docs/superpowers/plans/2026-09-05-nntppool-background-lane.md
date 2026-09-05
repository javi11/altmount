# Background Request Lane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop PAR2 repair from starving playback by adding a third, lowest-priority request lane to nntppool and moving repair traffic onto it.

**Architecture:** nntppool already has two lanes (priority, normal) read in strict preference by every connection writer. A `background` lane is read only after both are found empty, and only while the provider group's background in-flight count is under a floor whenever any foreground request was dispatched in the last few seconds. When nothing else is asking, background may use every connection. altmount then calls `BodyBackground` and `StatMany{Background: true}` from par2repair and drops its caller-side workarounds.

**Tech Stack:** Go 1.25+, nntppool v4 (`~/mio/nntppool`), altmount (`internal/par2repair`, `internal/pool`, `cmd/altmount/cmd/setup.go`).

**Spec:** Conversation decision of 2026-09-05: scheduler-level priority with a background floor of one quarter of the group, implemented in the library, not in the caller.

## Global Constraints

- Conventional Commits; branches `feat/background-lane` (nntppool) and `perf/par2repair-background-lane` (altmount).
- No competitor names anywhere in code, comments, or commits.
- nntppool: `go test ./... -count=1` green and `go tool golangci-lint run` clean before each commit.
- Never trade correctness: every request that increments the background in-flight counter must decrement it on every exit path (reader completion and pending drain).

---

### Task 1 (nntppool): lane type and strict-preference read order

**Files:**
- Modify: `nntp.go` (Request struct ~line 214-222, `NNTPConnection` struct ~259, `tryNextRequest` ~500, writer blocking select ~1200, `runConnSlot` ~805-845 and ~928, `startProviderGroup` ~2140, `tryGroupTimeout` ~2700, `sendSync`, `tryGroup`, `tryGroupResilient`, `doSendWithRetry`)
- Modify: `stat.go` (`statOne`, `statViaGroup`, `StatManyOptions`, `StatMany`)
- Modify: `client.go` (new `SendBackground`, `BodyBackground`, `StatBackground`)
- Modify: `metrics.go` (`providerStats` gains `bgInflight atomic.Int32`, `lastForeground atomic.Int64`, `bgFloor int32`)
- Test: `background_lane_test.go`

**Interfaces produced:**
```go
type lane uint8
const ( laneNormal lane = iota; lanePriority; laneBackground )
func (c *Client) SendBackground(ctx, payload, bodyWriter, onMeta...) <-chan Response
func (c *Client) BodyBackground(ctx, messageID, onMeta...) (*ArticleBody, error)
func (c *Client) StatBackground(ctx, messageID) (*StatResult, error)
type StatManyOptions struct { ...; Background bool }
type Provider struct { ...; BackgroundFloor int } // 0 => max(1, Connections/4)
```

- [ ] Step 1: failing test `TestTryNextRequest_BackgroundAfterNormal` (100 trials: bg and normal both queued, normal always wins) plus `TestTryNextRequest_BackgroundServedWhenAlone`.
- [ ] Step 2: run, expect compile failure on `bgCh`.
- [ ] Step 3: add `lane` type, `Request.lane`, `NNTPConnection.bgCh`, probe `c.bgLane()` last in `tryNextRequest` and as a case in the blocking select; `bgLane()` returns `c.bgCh` when `c.stats == nil`.
- [ ] Step 4: green; commit `feat(lane): add background request lane read after priority and normal`.

### Task 2 (nntppool): background floor while foreground is recent

- [ ] Step 1: failing test `TestBackgroundLaneGate`: table over (`lastForeground` age, `bgInflight`, floor) → expect nil channel only when age < `backgroundYieldWindow` AND inflight >= floor.
- [ ] Step 2: run, expect failure (`bgLane` ignores stats).
- [ ] Step 3: implement `backgroundLaneFor(stats *providerStats, bgCh <-chan *Request)`; `backgroundYieldWindow = 3 * time.Second`.
- [ ] Step 4: green; commit `feat(lane): cap background in-flight while foreground traffic is recent`.

### Task 3 (nntppool): accounting on the wire

- [ ] Step 1: failing test `TestBackgroundInflightAccounting`: unit-level writer/reader with a pipe server; after one `BodyBackground` completes, `g.stats.bgInflight == 0` and `lastForeground` is untouched; after one `Body`, `lastForeground` is recent.
- [ ] Step 2: run, expect failure (counter never moves).
- [ ] Step 3: in both writer paths (bootstrap and main loop), after `bw.Write` succeeds: background → `stats.bgInflight.Add(1)`, `req.heldBg = true`; otherwise `stats.lastForeground.Store(now)`. Release in reader completion (next to `heldBody`) and in `drainPending`.
- [ ] Step 4: green; commit `feat(lane): track background in-flight per provider group`.

### Task 4 (nntppool): dispatch and public API

- [ ] Step 1: failing tests `TestBackgroundDispatchUsesBackgroundChannel` (stand-in receiver on `g.bgCh` gets the request; `hotIdleBodyCh`/`hotPrioCh` stand-ins do not) and `TestClient_BodyBackgroundRoundTrip` (real pipe server, decoded bytes match) and `TestStatManyBackgroundOption`.
- [ ] Step 2: run, expect compile failure on `BodyBackground`.
- [ ] Step 3: thread `lane` through `sendSync`, `tryGroup*`, `doSendWithRetry` (post-430 escalation: normal → priority, background stays background), `statOne`, `statViaGroup`; `g.bgCh` buffered `p.Connections`; `runConnSlot` idle wait includes the gated `bgCh`; `Provider.BackgroundFloor` resolved into `stats.bgFloor` in `startProviderGroup`.
- [ ] Step 4: `go test ./... -count=1` and `go tool golangci-lint run` green; commit `feat(client): BodyBackground, StatBackground, SendBackground and StatManyOptions.Background`.
- [ ] Step 5: tag `v4.23.0`, push branch and tag.

### Task 5 (altmount): repair on the background lane

**Files:**
- Modify: `go.mod` (nntppool v4.23.0)
- Modify: `internal/par2repair/fetch.go` (`BodyClient` uses `BodyBackground`; `statOnce` passes `Background: true`)
- Modify: `internal/par2repair/job.go` (remove `yieldFetchDepth` collapse from `fetchDepth`; keep fold-width yield)
- Modify: `cmd/altmount/cmd/setup.go` (drop the import-budget half of `CombineBudgets`)
- Modify: `internal/pool/pool.go` and fakes if `BodyBackground` must be on the `ConnectionPool` interface
- Test: `internal/par2repair/fetch_test.go`, `job_test.go`

- [ ] Step 1: failing test: fake `BodyClient` records which method was called; `PoolFetcher.Fetch` must call `BodyBackground`; `statOnce` must pass `Background: true`. `TestFetchDepthYieldsToStreams` inverted: depth no longer collapses when streams are active.
- [ ] Step 2: run, expect compile failures.
- [ ] Step 3: implement; update `Par2RepairConfig.MaxConnections` doc comment to say repair runs on the background lane.
- [ ] Step 4: `go build ./... && go test ./internal/par2repair/... ./internal/pool/...` green; commit `perf(par2repair): fetch on the pool's background lane and drop caller-side yielding`.
- [ ] Step 5: push, open PR.
