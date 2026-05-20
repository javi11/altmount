# Streaming & connection-management invariants

This document is the contract for AltMount's segment-download pipeline.
Each invariant below is pinned by a test; if you change streaming code,
re-read this file first and make sure none of the listed properties
regress. If you intentionally relax one, change the corresponding test in
the same PR so the change is explicit.

The primary observability primitive is **MaxInFlight** on the fake nntppool
client (see [`internal/testsupport/fakepool`](../testsupport/fakepool/fakepool.go)).
It records the high-water mark of concurrent `Body` / `BodyPriority` /
`BodyAsync` / `Stat` calls. Every invariant below is some assertion about
that counter under specific conditions.

---

## Scope

This document covers connection-lifecycle and retry behaviour for the
streaming and metadata-virtual-file paths: bounded retry, cancellation,
prefetch, ephemeral-read coalescing, closer-pool bounds, fast Close
under client disconnect, and the speed-test endpoint. Cross-process
budgeting between streams and imports (admission control) is out of
scope here — that lives in `pool.Manager` and is documented alongside
the admission controller itself.

---

## Currently pinned (passing tests)

### I1 — Per-reader prefetch bound under steady read
A `UsenetReader` configured with `maxPrefetch=N` MUST schedule at most `N`
concurrent segment downloads ahead of the current read position, even
when the consumer reads faster than the provider responds.

- Pinned by: [`TestPrefetch_RespectsMaxPrefetchUnderSteadyRead`](usenet_reader_prefetch_test.go)
- Pinned by: [`TestPrefetch_DoesNotExceedMaxPrefetchOnSlowPool`](usenet_reader_prefetch_test.go)

### I2 — One BodyPriority call per segment on a sequential read
End-to-end sequential read of a file MUST issue exactly one `BodyPriority`
call per segment. Any code path that double-fetches, or that creates two
readers for overlapping regions of the same stream, breaks this.

- Pinned by: [`TestSequentialRead_OneRequestPerSegment`](usenet_reader_sequential_test.go)

### I3 — `ErrArticleNotFound` is never retried
`nntppool.ErrArticleNotFound` is permanent. Retrying it wastes connections
on an answer that will never change. Per-message-ID call count for a
missing article MUST equal 1.

- Pinned by: [`TestRetry_ArticleNotFound_NoRetry`](usenet_reader_retry_test.go)

### I4 — Cancellation stops retries immediately
Closing a reader (or cancelling its context) MUST stop any in-flight
retry loop from issuing further `BodyPriority` calls. Without this,
closing a stream during a flaky-provider window would let the retry
loop keep firing requests after the consumer is gone.

- Pinned by: [`TestRetry_ContextCancellation_StopsImmediately`](usenet_reader_retry_test.go)

### I6 — Bounded retry (≤ 2 wire calls per failure) with jitter
A permanently failing non-`ErrArticleNotFound` segment MUST produce at
most 2 `BodyPriority` calls (one initial + one bounded retry). Inter-
attempt delays MUST carry random jitter so multiple readers retrying
simultaneously desynchronize rather than thundering-herd against a
recovering provider. Implemented as `retry.Attempts(2)` with
`retry.CombineDelay(BackOffDelay, RandomDelay)` and a 100ms
`retry.MaxJitter`. (Fix landed: was storms S1+S3.)

- Pinned by: [`TestStorm_RetryAmplifiesPerMessageCallCount`](usenet_reader_storm_test.go)
- Pinned by: [`TestStorm_RetryUsesFixedDelayInsteadOfExponentialBackoff`](usenet_reader_storm_test.go)

### I9 — Random ReadAt is coalesced by a per-file segment LRU
`MetadataVirtualFile.ReadAtContext`'s ephemeral path MUST coalesce
small reads within the same segment via a per-file LRU of full segment
bytes (`randomReadCacheSize=8`). Without this, every non-sequential
ReadAt issues a fresh `BodyPriority` for its containing segment, and
Plex/Jellyfin scrubbing (which produces bursts of small reads across a
handful of segments) drives wire traffic equal to the read count.
Cache is bypassed for encrypted and nested-source files because their
segment boundaries don't align with plaintext byte ranges. (Fix
landed: was storm S5.)

- Pinned by: [`TestStorm_RandomReadAtCreatesEphemeralReaderPerCall`](../nzbfilesystem/metadata_remote_file_storm_test.go)

### I10 — Bounded closer-worker pool absorbs seek-spam
`MetadataVirtualFile.closeCurrentReader` MUST hand the detached reader
to a per-file bounded closer pool (closerWorkerCount=4 workers) rather
than spawning an unbounded goroutine per close. Without the bound, a
50-seek burst against slow-closing readers produces ~50 live closer
goroutines, each pinned for the close duration (up to 30s waiting on
in-flight downloads). With the bound, peak goroutine growth stays at
a small constant regardless of seek rate. (Fix landed: was storm S6.)

- Pinned by: [`TestStorm_SeekSpamAccumulatesCloserGoroutines`](../nzbfilesystem/metadata_remote_file_storm_test.go)

### I11 — Speed-test endpoint routes through pool.Manager + singleflight
`handleTestProviderSpeed` MUST consult `pool.Manager` (so an active
provider reuses the running pool's connections) and otherwise route
through a process-wide `speedtestCoordinator` that dedupes concurrent
requests per providerID and caches the per-provider nntppool client
for a short TTL. Without this, every HTTP request opened a fresh
nntppool.Client with `MaxConnections` dial attempts — a monitoring
script could trivially exhaust the provider's connection budget.
(Fix landed: was storm S8.)

- Pinned by: [`TestStorm_SpeedTestBypassesPoolManager`](../api/provider_speedtest_storm_test.go)

### I12 — `MetadataVirtualFile.Close` returns fast on client disconnect
`MetadataVirtualFile.Close` MUST return within a small constant
(~250ms on a healthy host) regardless of segment latency, by firing
ctx-cancel on the in-flight `UsenetReader` via an atomic
`interruptHandle` BEFORE contending for `mvf.mu`. Without this, a
concurrent `Read` holding `mvf.mu` keeps `Close` blocked for the full
segment-download latency, multiplied across every disconnect in a
Plex/Jellyfin scrubbing session. (Fix landed: was storm S9.)

- Pinned by: [`TestStorm_ClientDisconnectHoldsPoolSlotForUpTo30s`](../nzbfilesystem/disconnect_storm_test.go)
- Pinned by: [`TestStorm_ConcurrentDisconnectsPinManyGoroutines`](../nzbfilesystem/disconnect_storm_test.go)

---

## Storms reproduced today (CURRENT-BEHAVIOR pins)

The connection-lifecycle storms covered by this document have been
fixed and their tests inverted to enforce the post-fix invariants
above. This section is the staging area for any new storm
reproductions discovered in the future: each entry asserts the CURRENT
bad behavior and prints a `storm reproduction:` log line. When a fix
lands, the assertion fails (because the bad behavior no longer
happens). The fix PR INVERTS the assertion in the same diff — the
comment block names the TARGET INVARIANT and provides the marker
`If this fires LOW/HIGH, the fix has landed — invert this assertion`.
After inversion the test moves up into "Currently pinned" to guard
the fix.

*(No storms outstanding.)*

---

## How to add a new invariant

1. Identify the connection-storm condition. Express it as a property of
   `MaxInFlight`, `PerMessageCalls`, `BodyCalls`, `BodyPriorityCalls`, or
   `runtime.NumGoroutine` (for closer/leak scenarios).
2. Add the test to either
   [`internal/usenet/`](usenet_reader_storm_test.go) (if the invariant is
   about `UsenetReader`) or
   [`internal/nzbfilesystem/`](../nzbfilesystem/metadata_remote_file_storm_test.go)
   (if it is about reader lifecycle in `MetadataVirtualFile`).
3. If the production code already satisfies the invariant, the test goes
   into the "Currently pinned" section near the top of this file.
4. If the storm exists today, write the test to **pin the CURRENT bad
   behavior** with a concrete assertion. Add a comment block that
   describes:
   - CURRENT BEHAVIOR (what the assertion checks today)
   - TARGET INVARIANT (what the assertion should check after the fix)
   - The marker `If this fires LOW/HIGH, the fix has landed — invert
     this assertion`.
   Then add an entry to the "Storms reproduced today" section above.
5. When the fix lands, the test fails. The fix author inverts the
   assertion (changes the comparison and the message) in the same PR
   and moves the entry to "Currently pinned".

---

## Test infrastructure reference

- [`internal/pool/nntpclient.go`](../pool/nntpclient.go) — narrow
  `NntpClient` interface. Production `*nntppool.Client` satisfies it; the
  fake satisfies it too. This is the only seam tests use.
- [`internal/testsupport/fakepool`](../testsupport/fakepool/fakepool.go)
  — deterministic in-process fake. Per-message-ID latency / error
  injection, in-flight counters, gate primitive for pinning concurrency.
- [`internal/testsupport/segments`](../testsupport/segments/segments.go)
  — deterministic payload/message-ID generator. `FileBytes(n, size)` is
  the reassembly oracle.
- [`internal/testsupport/goroutines`](../testsupport/goroutines/goroutines.go)
  — `Snapshot` + `AssertReturnedToBaseline` for closer-accumulation
  scenarios.
