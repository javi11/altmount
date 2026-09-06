# Priority-lane steering in the NNTP pool

**Date:** 2026-09-01
**Status:** Approved design, pending implementation plan
**Repos:** `github.com/javi11/nntppool` (phases 1a-1c), `github.com/javi11/altmount` (phases 1d, 1e, 2)

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

Five defects explain it. D1-D4 come from the latency measurements above; D5
came out of a CPU profile taken afterwards.

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

### D5 — `StatMany` dispatch serialises on one unbuffered channel

A CPU profile of the widest scenario (S4: 470 MB/s decoded plus 27 k STAT/s over
12.4 s wall, 18.1 s of samples) shows CPU is not where one would guess:

| Cost | Share of samples |
|---|---|
| yEnc decode (`decodeYenc`, already cgo/SIMD) | **2.0 %** |
| `syscall.rawsyscalln` (read + write) | 37.1 % |
| Scheduler park/wake — `kevent` 13.9 %, `pthread_cond_wait` 13.8 %, `usleep` 8.0 %, `pthread_cond_signal` 6.6 % | ~42 % |
| `runtime.lock2` (cum) / `runtime.selectgo` (cum) | 16.4 % / 10.7 % |
| GC | ~3 % |

Roughly half the syscall time is the in-process fake server, so the client's true
share is smaller — which makes the scheduler and lock figures proportionally
larger still. The decode everyone reaches for first is 2 %.

The `lock2`/`selectgo`/`cond_signal` signature points at `StatMany`
(`stat.go:119-134`), which feeds up to `conc` (4 096) worker goroutines from a
**single unbuffered channel**:

```go
ids := make(chan string)   // unbuffered
wg.Add(conc)
for range conc {
    go func() { for id := range ids { ... } }()
}
```

Every send is a runtime handoff that takes `hchan.lock` and wakes one goroutine
out of a 4 096-deep receive queue. The work is a fully-materialised slice, so a
channel buys nothing here — it is the streaming primitive applied to non-streaming
work.

This only bites in the wide-sweep path (S4/S5, idle pool), which is exactly where
phase 1d sends *more* traffic by reviving adaptive widening.

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

### Phase 1c — nntppool: lock-free STAT dispatch (D5)

Replace the `ids` channel with an atomic cursor into `messageIDs`. Each worker
pulls its own index; there is no channel, no `hchan.lock`, and no parking on the
dispatch side:

```go
var cursor atomic.Int64
for range conc {
    go func() {
        defer wg.Done()
        for {
            i := int(cursor.Add(1)) - 1
            if i >= len(messageIDs) || ctx.Err() != nil {
                return
            }
            ...
        }
    }()
}
```

One uncontended atomic add per message-id replaces a locked handoff. Cancellation
semantics are preserved: the existing loop broke out of dispatch on `ctx.Done()`,
and the worker's own `ctx.Err()` check plus the existing `select` on the `out`
send do the same job.

Two things deliberately left alone:

- **Worker count stays `conc`.** `statOne` blocks until its reply arrives, so one
  goroutine per outstanding STAT is inherent without an async API.
- **The `out` channel stays.** It is the public return type
  (`<-chan StatManyResult`) and is already buffered to `conc`. Fan-in contention
  there is real but cannot be removed without an API change; measure it after this
  phase rather than pre-emptively batching results and changing streaming
  semantics.

Cost when sweeps are narrow: identical — the cursor is simply less contended than
the channel it replaces.

### Phase 1d — AltMount: config correctness (D4)

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

### Phase 1e — AltMount: re-measure `ImportBudget` (D3)

Do not delete it up front. Re-run the benchmark after 1a-1c and check whether it
still never binds. If it remains inert, removing it is a separate, evidenced
change; if phase 2 raises import concurrency, it may become the right place to
bound memory rather than latency. **No code change in this phase — a measurement
and a decision.**

### Phase 2 — import body pipelining — ABANDONED, measured harmful (2026-09-01)

`ImportBudget` capacity is `TotalProviderConnections()` = 100, but the pool's body
capacity is `conns × Inflight` = 1000. Imports therefore run ~1 body per
connection and pay the full 40 ms TTFB serially per article: 750 KB / 134 ms. The
measured 458 MB/s matches (100 × 0.75 MB / 0.165 s).

At 2 bodies pipelined per connection, body 2's TTFB overlaps body 1's transfer and
the cycle becomes transfer-bound at ~94 ms → ~800 MB/s, roughly **+75 %**. This is
a model prediction, not a measurement.

**This phase is dead. Do not revive it without re-reading this section.**

The +75% above was an artifact of a harness with no aggregate bandwidth ceiling:
it throttled each connection independently, so 100 connections could reach
800 MB/s. Under a realistic 400 MB/s ceiling, doubling the import budget measured:

| | import MB/s | stream p99 | stream MB/s |
|---|---|---|---|
| budget 100 (default) | 359 | 245 ms | 29.4 |
| budget 200 (2x pipelining) | 371 (**+3.4%**) | **536 ms** (2.2x worse) | 18.7 (−36%) |

That is the signature of a saturated link: the extra in-flight bodies buy almost
no throughput because the wire was already the constraint, while they more than
double stream latency and take a third of playback's bandwidth. Deeper body
pipelining also makes D2 strictly worse by leaving even fewer body-free
connections.

What replaced it: `import.stream_headroom_connections`, which spends the pool's
slack on playback instead of trying to extract throughput that the link cannot
deliver.

## Verification gates

Each phase is judged by `BenchmarkContention` on the existing harness, `-count=3`,
compared with `benchstat` against the numbers in the Problem table.

| Phase | Must improve | Must not regress |
|---|---|---|
| 1a | stream p99 (S2, S3) | import MB/s, STAT/s, p50 |
| 1b | stream p99 (S2, S3) toward the 158 ms S1 baseline | import MB/s (458/445), STAT/s (561/1 111/26 575) |
| 1c | STAT/s in S4/S5; `lock2` + `selectgo` share of a fresh CPU profile | stream p99, import MB/s; result *completeness* and cancellation semantics (`StatMany` documents results as unordered — order is not a property to preserve) |
| 1d | health STAT/s in S5 (no stream), via adaptive widening | stream p50/p95/p99 and import MB/s in S3; `StatSweepConcurrency` provably reached |
| 1e | — (no code change: a re-measurement that decides `ImportBudget`'s fate) | — |
| 2 | import MB/s | stream p99 no worse than after 1b; peak RSS bounded |

**Phase 1 splits into two implementation plans**, along the repo boundary: 1a-1c
in nntppool (one release), then 1d-1e in AltMount against that release. They are
specced together because 1d's adaptive widening is what makes 1c's dispatch cost
matter, but they ship separately.

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
- nntppool: a `StatMany` test asserting every id yields exactly one result, no
  duplicates, and that a mid-sweep `ctx` cancellation stops dispatch and closes
  the channel — the properties the cursor rewrite must preserve. Run under
  `-race`.
- AltMount: a config test asserting `0` reaches `StatSweepConcurrency`, and a
  migration test asserting a legacy key is carried over and cleared.

## Release path

nntppool changes ship as a tagged release (current: `v4.18.0`, local checkout
`/Users/javi/mio/nntppool`, clean on `main`). AltMount develops against a
temporary `replace` directive and switches to the released version before its own
phase-1c PR merges; the `replace` must not land in `main`.

## Outcome (2026-09-01)

Phase 1 shipped to `perf/priority-dispatch` in nntppool (6 commits, untagged) and
**failed its gate**: stream p99 did not move (S2 209 -> 209 ms). A diagnostic
settled why - `idleBodyChan()` returned non-nil zero times across ~22k
evaluations, because 91% of connections held exactly one body and only 0.8% were
ever body-free. The steering is correct but had nowhere to steer to. D5's atomic
cursor did deliver (`lock2` 16.6% -> 15.1%).

The gate only produced a trustworthy answer once the harness gained an aggregate
bandwidth ceiling. Under a real link the dominant effect on stream latency is not
per-connection head-of-line blocking at all - it is bandwidth share. That is what
`import.stream_headroom_connections` addresses, and it is where the measured win
actually lives.

## Explicitly out of scope

- Multi-provider failover and retry-storm behaviour under 430-heavy sets. The
  benchmark is single-provider with no misses; that is a separate question the
  harness can now answer.
- `stat_inflight_requests` tuning. Measured flat 10→200 at 100 connections; the
  depth is unreachable until sweep concurrency exceeds connection count.
- `ImportAdmission` (`internal/pool/admission.go`). It bounds whole imports, not
  connections, and never engaged in any scenario.
- **Further CGO.** The profile in D5 settles it: yEnc decode is already cgo/SIMD
  and costs 2 % of CPU, and every remaining cost is syscalls or Go scheduling,
  which cgo worsens because its calls pin OS threads. io_uring against the 37 %
  syscall share is the only real candidate and is rejected here — Linux-only
  (AltMount ships darwin and windows), requires bypassing Go's netpoller, and
  targets CPU when the bottleneck is 40 ms of RTT.
- **TLS cost.** Unmeasured: the harness runs plain TCP while real providers use
  TLS. Go's `crypto/tls` uses hardware AES on arm64 and amd64, so an OpenSSL-via-cgo
  swap is unlikely to pay for its per-call overhead — but this is reasoning, not a
  measurement, and the harness would need a TLS mode to settle it.
