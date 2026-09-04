# Streaming demand shaping and first-byte latency

**Date:** 2026-09-04
**Status:** Approved design, pending implementation plan
**Repos:** `github.com/javi11/altmount` (all phases), `github.com/javi11/nntppool` (phase 2 convenience API, phase 7 depth guidance)
**Delivery:** one stacked PR per phase, each rebased on the previous; the top of the stack is the full build for manual testing.

## Problem

The wire layer is in good shape: per-connection pipelining with an idle-body lane,
SIMD in-place yEnc decode, STAT-race failover, import-time size normalization,
import-time archive flattening. The weaknesses are in how the **stream layer
shapes demand** on that wire layer:

| # | Defect | Where | Cost |
|---|---|---|---|
| D1 | A segment is all-or-nothing: readers see bytes only after the whole article is decoded | `internal/usenet/segment.go` `SetData` | one full article transfer of first-byte latency per open/seek (≈100 ms at 8 MB/s for 750 KB; seconds on 4 MiB parts over slow links) |
| D2 | Read-ahead is per reader: every `UsenetReader` owns `max_prefetch` (60) goroutines on the priority lane | `usenet_reader.go` `downloadManager` | N handles on one title push N×60 priority BODYs into a `Connections`-deep channel → dispatch timeouts → false failovers |
| D3 | Only two lanes: read-ahead of stream A outranks the demand read of stream B | same | cross-stream p99 stalls |
| D4 | No in-flight dedup: two handles on one file, or shared + ephemeral readers on one handle, download the same message-ID twice | `internal/usenet`, `internal/pool` | duplicate BODYs, wasted allowance |
| D5 | Fixed 60-segment window fires on every reader creation, including thumbnail/ffprobe probes that read 2 MB and leave | `downloadManager` | ≈45 MB of speculative fetch per probe, contending with the one article the probe needs |
| D6 | No shared in-memory decoded-segment cache; disk `segcache` is off by default, `Put` is synchronous on the download goroutine, hits are `os.ReadFile` | `internal/nzbfilesystem/segcache` | cross-handle re-fetch, write latency in the fetch path |
| D7 | Persisted holes have no TTL and no provider fingerprint | `internal/holes`, `meta.KnownHoles` | backfilled or reposted articles stay padded forever; adding a provider does not un-hole |
| D8 | No TLS session cache, hardcoded 60 s idle, `min_connections_alive` default 0 | `internal/config/manager.go` `ToNNTPProvider` | a stream started after a minute idle pays TCP + full TLS + AUTHINFO |
| D9 | `inflight_requests` default 10 bodies per connection with FIFO replies | same | a demand BODY can land behind 9 × 750 KB on one connection |
| D10 | Reader distinguishes only `ErrArticleNotFound`; mixed transport/430 outcomes across providers are not audited | `usenet_reader.go` | risk of padding a segment a provider merely timed out on |

Plus housekeeping found on the way: dead `headroomController`, `config.sample.yaml`
`max_prefetch: 30` vs default 60, `GetBufferedOffset` re-materialising a consumed
lazy slot, cgofuse reads with `context.Background()`, provider removed on 502
never reconnecting (`ReconnectDelay` unset).

## Non-negotiable constraint

**No phase may reduce throughput or increase first-byte/seek latency** as measured
by the benchmark in phase 0 and the live A/B script. A phase that regresses is
fixed or dropped before the next phase is stacked on it.

## Phase 0 — streaming benchmark and live A/B

New package `internal/streambench` (test-only, `//go:build bench` not required;
benchmarks skip unless `-bench` is set) driving the real stack:
`MetadataVirtualFile` → `UsenetReader` → `pool.Manager` → `nntppool.Client` →
`internal/testsupport/nntpserver`.

Provider model reuses the constants from `internal/pool/contention_bench_test.go`
(40 ms RTT ± 10 ms, 8 MB/s per connection, 400 MB/s aggregate, 750 KB articles,
50 connections). A second profile models 4 MiB articles at 100 ms RTT for the
progressive-delivery case.

Scenarios and metrics (p50/p99 unless noted):

| Scenario | Metrics |
|---|---|
| B1 cold open, read first 1 MB | TTFB, articles fetched |
| B2 single sequential 200 MB stream | MB/s, fetched/read byte ratio (waste) |
| B3 seek storm: 20 random 64 KB reads (Plex probe pattern) | per-read latency, articles fetched |
| B4 four concurrent streams, 50 conns | per-stream MB/s min/max, p99 read stall |
| B5 same file, two handles reading in lockstep | duplicate BODY count (server counter vs unique ids) |
| B6 stream + import + repair fetch contention | stream p99, import MB/s |
| B7 provider A missing 10 %, provider B has all | TTFB on a miss, fetches per miss |
| B8 pause/resume: read 20 MB, sleep 5 s, resume | bytes fetched during pause |

Output: JSON to `bench/results/<short-sha>.json` plus a `compare` subcommand
(`go run ./internal/streambench/cmd/compare base.json new.json`) printing deltas
and exiting non-zero on >5 % throughput loss or >5 % TTFB/seek p50 increase.
`make bench-stream` and `make bench-compare BASE=<sha>` wrap them.

Live A/B: `~/altmount-dev/bench-live.sh` (outside the repo) runs, against the
dev server's WebDAV endpoint, `curl -r` ranged reads (TTFB from `%{time_starttransfer}`),
a 60 s sequential `curl` throughput read, and `ffprobe` on three files; prints
the same table. Run once on `main` for baseline, once per phase.

## Phase 1 — connection hygiene (D8)

`ToNNTPProvider`: `TLSConfig.ClientSessionCache = tls.NewLRUClientSessionCache(max_connections)`;
`IdleTimeout` 60 s → 120 s (providers drop idle sockets around 3 minutes);
`min_connections_alive` default 0 → 2 when unset, capped at `max_connections`.
Config docs updated. Expected: B1 TTFB after idle drops by one TLS handshake.

## Phase 2 — progressive segment delivery (D1)

`segment` gains a watermark: `buf []byte` (pre-sized from the yEnc `=ypart`
size via `onMeta`, else `SegmentSize`), `ready int64` (bytes published), a
`sync.Cond` or per-waiter channel, and `Write(p)` appending and publishing.
Publication cadence: every 32 KiB or when the decoder hands over a chunk smaller
than that (the pool's `readBuffer` feeds whatever the socket had, so slow links
publish on arrival).

`GetReaderContext` becomes a reader that serves `[Start, End]` as bytes become
available: it waits for `ready > Start + consumed`, copies, and returns `io.EOF`
only after the article is finished. The segment's `Start` trim means the first
read waits for `Start+1` bytes, not the whole article.

Fetch: `downloadSegmentWithRetry` calls a new pool method
`BodyPriorityStream(ctx, id, w, onMeta)` (nntppool adds it as a thin wrapper over
`SendPriority` with a writer; import keeps buffered `Body`). nntppool already
refuses to fail over after partial delivery and returns the error; the reader's
retry loop then starts a **second attempt into a fresh buffer** and only advances
`ready` once the new buffer passes the already-published watermark (bytes below
it are identical by definition, same message-ID). A CRC mismatch at the end
surfaces as today; readers that consumed the prefix were handed the same bytes
they would have received a moment later.

`segcache.Put` and `PatchLookup` unchanged in this phase (phase 6 moves them).
`randomReadCache` stores the finished buffer as today.

Expected: B1 TTFB on the 4 MiB/100 ms profile drops from ≈1.3 s to ≈one RTT plus
first chunk; B2 throughput unchanged.

## Phase 3 — shared read-ahead budget and demand tiering (D2, D3)

New `pool.SpeculativeBudget` (reuse `adaptiveSemaphore`): capacity
`TotalProviderConnections() × min(inflight, 3) − 1`, non-blocking `TryAcquire`.
Owned by `pool.Manager`, exposed through `pool.NntpClient`'s getter so
`UsenetReader` receives it via a `ReaderOption`.

`downloadManager` classifies each segment:

- **demand**: the segment the reader is blocked on plus the next `demandDepth = 2`
  → `BodyPriorityStream`, no budget.
- **speculative**: everything else up to `maxPrefetch` → must `TryAcquire` a
  budget slot; on failure the manager skips it this round and re-checks when the
  reader advances (cond signal) rather than queueing. Speculative fetches use the
  **normal** lane (`BodyStream`), so another stream's demand read outranks them
  inside the pool, and release the slot on completion.

`maxPrefetch` remains the per-reader ceiling; the budget is the pool-wide one.
Import readers (`WithImportProfile`) keep the `ImportBudget` and are unaffected.

Expected: B4 per-stream min/max spread narrows, B4 p99 stall drops, B2 unchanged
(a single stream still fills its window because the budget equals the allowance).

## Phase 4 — seek ramp (D5)

`downloadManager` window after reader creation or `Seek`: `2 << (2 × consumed)`
segments, capped at `maxPrefetch`, where `consumed` is segments fully read since
the anchor. A range hint (`MetadataVirtualFile` knows the requested length for
WebDAV; FUSE passes 0) of ≥128 MiB skips the ramp. `randomReadCache` and
`ephemeralStreakLimit` stay.

Expected: B3 articles fetched per probe drops from ≈60 to ≈3; B2 unchanged.

## Phase 5 — in-flight dedup with supersede (D4)

`pool.FlightMap` keyed by message-ID inside `pool.Manager`, consulted from
`downloadSegmentWithRetry` before hitting the pool:

- an existing flight → **join**: the joiner attaches to the leader's segment
  buffer (watermark from phase 2 makes joining mid-transfer natural).
- a demand caller finding a **speculative flight that has not yet dispatched**
  (nntppool exposes `Request` dispatch state through the `onMeta`/first-byte
  callback: no bytes yet ⇒ queued) → **supersede**: cancel the speculative
  attempt's context, start a demand attempt, followers move to it.
- demand caller finding a downloading flight → join.

Bounded by three attempts per key. Entries are removed on completion.

Expected: B5 duplicate BODY count → 0; B1 unchanged or better.

## Phase 6 — decoded-segment memory cache (D6)

`internal/nzbfilesystem/segcache` gains an in-memory tier: sharded (16) map of
message-ID → `[]byte`, byte-bounded (`segment_cache.memory_mb`, default 256,
accounted by `cap`), CLOCK second-chance eviction (hit sets a touched flag under
a read lock; the sweep reorders). It sits in front of the disk tier and is on
even when the disk tier is disabled. Disk `Put` moves to a bounded async writer
(queue 64, drop on overflow with a counter). `randomReadCache` is removed in
favour of the shared tier. `debug.SetMemoryLimit` is derived when `GOMEMLIMIT`
is unset: memory tier + prefetch ceiling + 256 MiB headroom.

Expected: B5 fetches for the second handle → near 0 even without dedup; B3
repeat probes hit memory.

## Phase 7 — per-connection body depth for streams (D9)

nntppool: add `Provider.StreamInflight` (default 4) applied to priority-lane
bodies while normal-lane bodies keep `Inflight`; documented in the config
reference. altmount: expose `providers[].stream_inflight_requests`.

Expected: B6 stream p99 drops; import MB/s unchanged.

## Phase 8 — hole records with TTL and provider fingerprint (D7)

`KnownHoles` entries gain `recorded_at` and the metadata file gains
`hole_provider_fingerprint` (SHA-1 of sorted enabled provider hosts, 8 bytes hex).
On open, holes older than 24 h or recorded under a different fingerprint are
treated as **unknown** (re-probed on first touch, one read pays the walk, the
rest wait on the flight from phase 5). Transport failures never create or
refresh a hole (already true; add a test).

## Phase 9 — error ranking audit and housekeeping (D10 + small fixes)

- Add tests asserting that when one provider times out and another returns 430,
  the reader does **not** pad (nntppool `keepErr` path); fix if it does.
- Delete `internal/pool/headroom.go` (dead) or wire it; decision: delete, the
  static reserve plus phase 3 covers the need.
- `config.sample.yaml` `max_prefetch` → 60.
- `GetBufferedOffset`: read the slot without materialising it.
- cgofuse `Read`: derive a cancellable context from the handle and cancel on
  `Release`/`Flush`.
- `ToNNTPProvider`: set `ReconnectDelay` (30 s) so a 502-removed provider returns.

## Testing

Every phase: table-driven unit tests in the touched package, the existing
`internal/usenet` invariant tests (`storm`, `lane`, `prefetch`, `sequential`,
`race`) stay green under `-race`, `make` passes, and `make bench-compare` against
the previous phase's JSON shows no regression. Phase-specific assertions are
listed above under "Expected".

## Out of scope

Dashboard UI for new knobs (config file only), nntppool dispatch-policy changes,
import path changes beyond keeping its interfaces compiling.
