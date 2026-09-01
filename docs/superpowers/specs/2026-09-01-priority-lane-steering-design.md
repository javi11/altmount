# Priority-lane steering in the NNTP pool

**Date:** 2026-09-01
**Status:** Approved design, pending implementation plan
**Repos:** `github.com/javi11/nntppool` (phases 1a, 1b), `github.com/javi11/altmount` (phases 1c, 1d, 2)

## Problem

Benchmarking `internal/pool` against a real `nntppool.Client` at 100 connections,
40 ms RTT, 8 MB/s per connection, 750 KB articles (harness:
`internal/testsupport/nntpserver`, benchmarks: `internal/pool/contention_bench_test.go`)
produced these medians over 3×15 s runs:

| Scenario | p50 | p95 | p99 | stream MB/s | import MB/s | STAT/s |
|---|---|---|---|---|---|---|
| S1 stream only | 146 | 156 | 158 | 39.8 | — | — |
| S2 stream + import | 143 | 201 | 210 | 37.6 | 458 | 561 |
| S3 stream + import + health | 153 | 202 | 217 | 36.5 | 445 | 1 111 |
| S4 import + health, no stream | — | — | — | — | 464 | 26 575 |
| S5 health only, no stream | — | — | — | — | — | 1 947 |

Stream p50 moves +5 %; p99 moves +37 %. Decomposing the 59 ms of p99 inflation:
**52 ms comes from import bodies (S1→S2), 7 ms from STAT sweeps (S2→S3).** Zero
dispatch timeouts and zero errors across all 39 runs — nothing starves, but the
tail is real.

Four defects explain it.

### D1 — the priority lane is not actually prioritised

`NNTPConnection.tryNextRequest` (`nntp.go:457-476`) probes `hotPrioCh`, then
`hotReqCh`, then a **single unbiased `select`** over `prioCh` and `reqCh`:

```go
select {
case req, ok = <-c.prioCh:
    return req, ok, true
case req, ok = <-c.reqCh:
    return req, ok, true
default:
}
```

Go picks uniformly among ready cases, so once both lanes have work the "priority"
lane wins a coin flip. Separately, `hotReqCh` is probed *before* `prioCh`, so a
normal request bound for a hot connection outranks a priority request bound for a
cold one.

The cold-slot path in `runConnSlot` (`nntp.go:714-726`) already does this
correctly — a non-blocking `prioCh` probe, and only then the unbiased select.
`tryNextRequest` is missing a shape that exists 250 lines above it.

### D2 — a priority body can queue behind a normal body on the same connection

This is the dominant cost. `pending` is a FIFO channel and `readerLoop` must drain
a whole body before it can parse the next reply. At 8 MB/s a 750 KB article holds
its connection's reader for ~94 ms, and everything behind it waits.

`hotPrioCh` does not avoid this. It is unbuffered and only a connection parked in
the writer's blocking select (`nntp.go:1060-1064`) can receive from it — but the
writer parks while its *reader* may still be draining a body. "Parked writer" is
not "free reader".

So a priority body only escapes the 94 ms if it lands on a connection with **no
body-bearing request outstanding**, and nothing in the dispatch path steers it
there.

### D3 — AltMount's `ImportBudget` cannot fix D2, and is inert as configured

`ImportBudget` (`internal/pool/budget.go`) is a pool-wide token count with
`effective cap = capacity − min(streamHeadroom × activeStreams, capacity−1)` and
`streamHeadroom = 2`. At 100 connections one active stream takes the cap from 100
to 98. Measured import throughput was 458 MB/s with a stream and 464 MB/s without
— the budget never bound.

It also cannot bind usefully, because it counts tokens without knowing *which*
connection a body lands on. Only per-connection state can answer D2.

### D4 — the health sweep's adaptive branch is dead code

`HealthChecker.statSweepConcurrency` (`internal/health/checker.go:417`):

```go
if cfg.Health.MaxConnectionsForHealthChecks > 0 {
    return cfg.Health.MaxConnectionsForHealthChecks
}
return hc.poolManager.StatSweepConcurrency(cfg.StatConcurrency())
```

Its comment promises "otherwise the pool manager adapts", but the default is `100`
(`config/manager.go:1860`) and validation *rejects* `<= 0`
(`config/manager.go:971-973`). The second `return` is unreachable in every valid
config. Health sweeps never narrow when a stream starts and never widen when the
pool is idle; only the import fast-fail sweep gets that behaviour
(`importer/processor.go:214`).

The knob is also misnamed. `max_connections_for_health_checks` bounds STAT
*concurrency*, not connections — `config.sample.yaml:168` already documents it
correctly ("Max concurrent STAT checks within a single sweep"); only the name is
stale. The import path made this correction explicitly
(`processor.go:208-212`: *"Size the sweep by the providers' STAT pipeline depth,
not their connection count"*); health did not.

Consequence at 100 connections: a sweep at concurrency 100 issues **one STAT per
connection**. The benchmark observed a peak per-connection pipeline depth of 4
against a `stat_inflight_requests` cap of 100, and varying that knob 10→200
changed nothing (p99 212/213/210/212 ms; 1 115/1 116/1 123/1 123 STAT/s). The
STAT pipelining the knob configures is unreachable until sweep concurrency exceeds
the connection count.

## Constraint

**No throughput regression.** Import MB/s and aggregate STAT/s must not fall. This
rules out any static reservation: capacity held back from the normal lane while no
stream is playing is pure waste. Every mechanism below must cost *nothing* when no
priority traffic exists.

## Architecture

Fix D1 and D2 where they live — in nntppool's dispatch — rather than compensating
for them in AltMount with a blunter instrument.

### Phase 1a — nntppool: strict lane ordering (D1)

Two changes in `tryNextRequest`:

1. Probe `prioCh` non-blocking before the unbiased select, mirroring
   `runConnSlot`'s cold path.
2. Reorder to `hotPrioCh` → `prioCh` → `hotReqCh` → `reqCh`, so no normal request
   can outrank a priority one.

The blocking select at `nntp.go:1060-1064` gets the same treatment: a non-blocking
priority probe before falling into the multi-way select.

Cost when no priority traffic exists: one `select` whose cases are never ready.

### Phase 1b — nntppool: body-free steering (D2)

Add a fifth group channel, `hotIdleBodyCh chan *Request` (unbuffered), alongside
the existing four (`nntp.go:1883-1886`).

**Receive side.** In the writer's park-select, the connection offers this channel
only while it has no body outstanding:

```go
var idleBodyCh <-chan *Request
if len(c.bodySem) == 0 {
    idleBodyCh = c.hotIdleBodyCh
}
select {
case req, ok = <-idleBodyCh:   // nil when busy → never ready
...
```

A `nil` channel is never ready in a `select`, so a connection with a body in
flight simply does not participate. No flag to maintain, no lock.

`len(c.bodySem)` is a sound "reader is free" signal: the slot is released in
`readerLoop` at `nntp.go:1432-1434`, *after* the full body has been read and
delivered. The window where the writer has taken a slot but the reader has not
started counts as busy — conservative in the correct direction.

**Send side.** In `tryGroupResilient` (`nntp.go:2374-2382`), when the request is
priority *and* body-bearing (`!isCheapCommand(payload)`), try `hotIdleBodyCh`
first, then `hotPrioCh`, then the cold `prioCh`. Priority STATs and all
normal-lane traffic keep today's path unchanged.

Cost when no priority traffic exists: nothing is ever sent on `hotIdleBodyCh`, so
the extra select case never fires and no connection is withheld from the normal
lane. Capacity is *steered*, never reserved.

### Phase 1c — AltMount: config correctness (D4)

1. Allow `max_connections_for_health_checks: 0` in validation and make `0` mean
   "adapt", reviving `StatSweepConcurrency`. Change the default to `0` so new
   installs adapt; a migration preserves an explicit operator value.

   **This is a behaviour change, not pure correctness.** With `0`, an idle pool
   widens health sweeps from 100 to `StatCapacity()` (4 096 at this provider
   shape). The benchmark measured that widening on the *import* sweep as a 24×
   throughput gain (S4: 26 575 STAT/s) at no stream cost, and the health-knob
   sweep showed 100→500 costs nothing measurable (p99 213→214 ms). Both support
   the change, but the gate below must assert it rather than assume it.
2. Rename the field to match what it bounds (e.g. `health.stat_sweep_concurrency`),
   with a migration in the established `migrateArrsCleanup` / `migrateGlobalUserAgent`
   style (`config/manager.go:689`, `:679`, applied at `:1610-1612`): read the legacy
   key, populate the new one, clear the legacy field so it drops from saved YAML.
   Idempotent.
3. Give the frontend an explicit **Auto** affordance. The UI copy is already
   correct — the field is labelled "Max Concurrent Segment Checks" and its help
   text already says STAT "can be much higher than your provider's connection
   count" (`HealthConfigSection.tsx:778,794-795`) — but `min={1}` and the
   `?? 100` / `|| 100` fallbacks (`:783,789`) make `0` unreachable from the UI.
   Needs an Auto checkbox or a `0 = auto` affordance that round-trips.

4. Propagate the rename: `frontend/src/types/config.ts:114,398`,
   `frontend/src/components/config/HealthConfigSection.tsx:783-790`,
   `config.sample.yaml:168`, `docs/docs/3. Configuration/health-monitoring.md:290`,
   `docs/static/swagger.yaml:1121`, `docs/static/openapi.yaml:4367`.

### Phase 1d — AltMount: re-measure `ImportBudget` (D3)

Do not delete it up front. Re-run the benchmark after 1a/1b and check whether it
still never binds. If it remains inert, removing it is a separate, evidenced
change; if phase 2 raises import concurrency, it may become the right place to
bound memory rather than latency. **No code change in this phase — a measurement
and a decision.**

### Phase 2 — import body pipelining (gated)

`ImportBudget` capacity is `TotalProviderConnections()` = 100, but the pool's body
capacity is `conns × Inflight` = 1000. Imports therefore run ~1 body per
connection and pay the full 40 ms TTFB serially per article: 750 KB / 134 ms. The
measured 458 MB/s matches (100 × 0.75 MB / 0.165 s).

At 2 bodies pipelined per connection, body 2's TTFB overlaps body 1's transfer and
the cycle becomes transfer-bound at ~94 ms → ~800 MB/s, roughly **+75 %**. This is
a model prediction, not a measurement.

Deeper body pipelining makes D2 strictly worse, so this phase ships **only after**
phase 1 and **only if** its gate passes. It also raises peak memory (more
concurrent decoded article buffers), which the gate must watch.

## Verification gates

Each phase is judged by `BenchmarkContention` on the existing harness, `-count=3`,
compared with `benchstat` against the numbers in the Problem table.

| Phase | Must improve | Must not regress |
|---|---|---|
| 1a | stream p99 (S2, S3) | import MB/s, STAT/s, p50 |
| 1b | stream p99 (S2, S3) toward the 158 ms S1 baseline | import MB/s (458/445), STAT/s (561/1 111/26 575) |
| 1c | health STAT/s in S5 (no stream), via adaptive widening | stream p50/p95/p99 and import MB/s in S3; `StatSweepConcurrency` provably reached |
| 2 | import MB/s | stream p99 no worse than after 1b; peak RSS bounded |

Phase 2 is abandoned if its gate fails, and gets its own implementation plan once
phase 1's numbers are in — it is specced here only so the phase-1 gates are chosen
with it in mind.

Non-benchmark gates, every phase: `go build ./...`, `go vet ./...`,
`go test -race ./...` in both repos; nntppool's existing suite
(`nntp_test.go` 71 tests, `stat_test.go` 10, `client_test.go` 3) stays green.

New tests required:

- nntppool: a lane-ordering test asserting a priority request queued alongside a
  saturated normal lane is dispatched first (deterministic, no timing).
- nntppool: a steering test asserting a priority body never lands on a connection
  with `len(bodySem) > 0` while a body-free connection exists.
- AltMount: a config test asserting `0` reaches `StatSweepConcurrency`, and a
  migration test asserting a legacy key is carried over and cleared.

## Release path

nntppool changes ship as a tagged release (current: `v4.18.0`, local checkout
`/Users/javi/mio/nntppool`, clean on `main`). AltMount develops against a
temporary `replace` directive and switches to the released version before its own
phase-1c PR merges; the `replace` must not land in `main`.

## Explicitly out of scope

- Multi-provider failover and retry-storm behaviour under 430-heavy sets. The
  benchmark is single-provider with no misses; that is a separate question the
  harness can now answer.
- `stat_inflight_requests` tuning. Measured flat 10→200 at 100 connections; the
  depth is unreachable until sweep concurrency exceeds connection count.
- `ImportAdmission` (`internal/pool/admission.go`). It bounds whole imports, not
  connections, and never engaged in any scenario.
