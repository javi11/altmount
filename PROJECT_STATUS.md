# Altmount Delivery Status

**Last updated:** 2026-08-22
**Repository:** `javi11/altmount`
**Fork:** `nrlcode/altmount`
**Upstream base:** `main`

This file is the project-space ledger for the health-deduplication and availability work. It records what has shipped to review, what each PR owns, decisions already made, unresolved design questions, and the next safe checkpoint.

## Current checkpoint

- The health-deduplication change is committed and pushed to the fork as `feat/full-import-health-dedup`; upstream PR #833 is open.
- The availability foundation is committed and pushed as `feat/availability-foundation`; upstream PR #834 is open.
- No availability writer, health-check integration, streaming integration, hot cache, or STAT look-ahead work has started.
- Before dispatching the next implementation PR, conduct the architecture interview recorded below and update this file with the answers.

## PR ledger

### PR 1 — Full-import health-check deduplication

**Status:** Open upstream PR
**Branch:** `nrlcode:feat/full-import-health-dedup`
**Commit:** `2980f6c` (`feat: deduplicate full-import health validation`)
**PR:** https://github.com/javi11/altmount/pull/833

**Owns:**

- A narrow transient full-validation receipt.
- Exact current segment identity matching.
- Suppression of only the redundant immediate 100%-health initialization.
- Normal health behavior for partial, sampled, stale, mismatched, re-imported, PAR2, and degraded cases.

**Does not own:** metadata mtime behavior, unrelated API lint findings, availability persistence, provider scope, streaming, or article look-ahead.

**Verification:**

- Focused importer, scheduler/database, health, package, race, build, and diff checks passed.
- `make build` passed.
- `make test` still reaches the existing `internal/metadata/TestDirectoryModTime_StableAcrossHealthSweep` failure.
- `make lint` and `make check` still report existing findings in unchanged `internal/api` files.
- No merge or deployment has been performed.

### PR 2 — Availability foundation

**Status:** Open upstream PR
**Branch:** `feat/availability-foundation`
**Worktree:** `/home/hermes/workspaces/altmount-availability-foundation`
**Plan:** `.hermes/plans/2026-08-22_134300-availability-foundation.md`
**Commit:** `49b78d2` (`feat: add availability persistence foundation`)
**PR:** https://github.com/javi11/altmount/pull/834

**Owns:**

- Pure `internal/availability` identity and eligibility primitives.
- Non-secret aggregate active-provider-pool scope digest.
- Process/config scope generation seam.
- Canonical v1 manifest digest from resolved main-file `SegmentData`.
- Additive/reversible SQLite and PostgreSQL migration `035`.
- TTL-aware, idempotent `AvailabilityRepository`.
- Definitive manifest-present and article-confirmed-missing persistence only.
- Database constructor wiring, with no production callers yet.

**Explicitly does not own:**

- Import validation writers.
- Scheduled health-check writers.
- Streaming/open/read behavior.
- STAT look-ahead, `BodyPriority`, hot cache, fallback, repair, `KnownHoles`, `file_health`, `.meta`, metrics, public API/config, or scheduler work.

**Audit evidence:**

- Terra auditor: PASS (`t_3ecd55d5`).
- Independent Grok 4.6 auditor: PASS (`t_183ac982`).
- Focused tests, race tests, migration/repository tests, v3 metadata resolution, vet, build, formatting, and diff checks passed.
- PostgreSQL live execution was not run because the repository has no supported PostgreSQL test DSN/service/CI convention. Both dialect migration files received static parity checks; no DSN, credential, service, or CI setup was invented.
- The existing metadata mtime failure and API lint findings remain outside this PR.

**Next action:** monitor PR #834 alongside PR #833; do not merge automatically. The next implementation PR remains gated on the architecture interview below.

### PR 3 — Import and scheduled-health availability writers

**Status:** Not planned or dispatched; architecture interview required first

**Candidate ownership:**

- Write a complete manifest-present summary after successful full validation.
- Write sparse confirmed-missing article facts from health evidence only when the outcome is definitive.
- Keep degraded, sampled, canceled, timed-out, authentication, and provider-pool failures as unknown/uncertain.
- Define invalidation and refresh behavior when manifests or provider scope change.
- Reuse PR 2 identity and repository APIs without changing streaming behavior.

**Hard boundary:** this PR must not make streaming wait for validation, change first-byte behavior, or add the streaming hot cache/look-ahead path.

### PR 4 — Streaming integration and bounded STAT look-ahead

**Status:** Not planned or dispatched; architecture interview required first

**Candidate ownership:**

- Read fresh availability summaries and sparse negatives.
- Add a bounded in-memory hot cache for active streams.
- Start asynchronous, deduplicated, batched STAT look-ahead when knowledge is absent or stale.
- Preserve first-byte delivery and existing body-read behavior.
- Bypass futile body requests only for fresh confirmed negatives.
- Revalidate targeted STAT after body-fetch failure before declaring absence.
- Keep look-ahead separate from `BodyPriority` with cancellation, queue, rate, and concurrency caps.

### PR 5 — Optional policy/API/observability

**Status:** Not proposed for dispatch

Only plan this if the preceding PRs demonstrate a need for public policy, configuration, metrics, admin inspection, or repair controls. Do not add these knobs speculatively.

## Decisions already made

1. Health deduplication and persistent availability knowledge remain separate PRs and branches.
2. Availability persistence stores metadata/facts only, never article bodies.
3. Initial domain outcomes distinguish `present`, `confirmed_missing`, and `unknown`; uncertainty must not be converted into confirmed absence.
4. A complete manifest-present summary is preferred over a durable positive row for every article. Negative article facts are sparse.
5. Facts are scoped by canonical manifest identity and provider-pool identity/generation, with expiry and explicit invalidation boundaries.
6. Import validation and scheduled health checks are the first reusable writers; streaming is a consumer and bounded knowledge refresher.
7. Streaming must not block first-byte delivery on complete-file validation or a full STAT sweep.
8. Timeout, cancellation, authentication failure, provider-pool failure, and body-fetch uncertainty are not confirmed article absence.
9. STAT success does not prove body integrity.
10. STAT look-ahead must be bounded, batched, deduplicated, cancelable, rate-limited, and separate from body-priority scheduling.
11. Ordinary `Par2Files` sidecars do not change main-file identity. Unsafe main-file structures fail closed.
12. PR 2 uses an aggregate active-provider-pool scope, not per-provider facts. Active providers are canonicalized and hashed without storing passwords or raw credentials.
13. PR 2 rotates a fresh process/config generation when provider configuration differences or provider ordering changes are observed. The callback seam is not registered until a later integration PR.
14. PostgreSQL is supported at runtime, but live PostgreSQL tests are not a PR 2 gate until the repository defines a supported test service/DSN convention. No credentials or test infrastructure were invented.
15. The unrelated metadata mtime failure and existing API lint findings remain outside these PRs unless separately authorized.

## Architecture interview — decisions needed before PR 3

Record the answer and date under each item before dispatching the next planner.

### A. What is a provider for availability identity?

**Current PR 2 proposal:** a provider is represented in the aggregate pool digest by its stable ID plus normalized host, port, TLS/insecure-TLS, backup/storage-group, ping/connection/stat/keepalive/quota/user-agent settings, and a digest of the username. Password material is excluded.

**Decision needed:** Which fields define the identity of a provider for article availability? Should changing operational limits or keepalive settings invalidate facts, or should only endpoint/authentication/pool-membership changes do so?

**Answer:** _Pending interview._

### B. Does a provider change invalidate the segment/availability map?

**Current PR 2 behavior:** an observed provider diff or provider-order change creates a new generation, so old facts are not reused under the new scope/generation. Process restart also starts a fresh generation.

**Decision needed:** Confirm whether every active-pool change invalidates availability facts, or whether some changes should preserve the map while only changing scheduling behavior. Distinguish provider content identity from operational configuration.

**Answer:** _Pending interview._

### C. Aggregate pool versus per-provider knowledge

**Current PR 2 behavior:** facts describe availability from the active aggregate pool. `StatMany` and future writers must not claim provider-specific knowledge unless the repository explicitly adds provider identity.

**Decision needed:** Is pool-level availability sufficient for the first integration PR, including backup providers, or is provider-level attribution required before streaming integration?

**Answer:** _Pending interview._

### D. Scope lifecycle across restart and configuration reload

**Current PR 2 behavior:** process startup creates a fresh generation; a future integration seam can rotate on provider configuration change. No callback is registered in PR 2.

**Decision needed:** Should restart always invalidate in-memory/durable reuse, or should a stable persisted pool identity permit reuse across restart when the active pool is equivalent?

**Answer:** _Pending interview._

### E. Negative-fact confidence and repair

**Current proposal:** only definitive health evidence or targeted, correctly classified STAT evidence may write `confirmed_missing`. Body-fetch failure alone writes no negative fact; it triggers uncertainty/revalidation in a later PR.

**Decision needed:** What evidence threshold is required for a negative fact, and when should existing fallback/repair behavior be invoked versus returning an error?

**Answer:** _Pending interview._

### F. Streaming latency budgets

**Current proposal:** serve the first byte without waiting; use bounded asynchronous look-ahead with explicit queue, concurrency, rate, batch-size, and cancellation limits.

**Decision needed:** Set operational caps and decide whether stale positives, stale negatives, or unknowns may trigger background refresh during an active stream.

**Answer:** _Pending interview._

### G. PostgreSQL verification and CI

**Current state:** both migration dialects are implemented and statically checked; no repository PostgreSQL test environment exists.

**Decision needed:** Is SQLite plus static dual-dialect parity sufficient for PR 2, with PostgreSQL integration deferred to a separate CI/infrastructure PR, or should that infrastructure be added before any writer PR?

**Answer:** _Pending interview._

### H. Failure taxonomy and evidence thresholds

**Current proposal:**

- `confirmed_missing`: only a definitive, correctly classified provider-pool observation supports this state.
- `transient/provider_error`: timeout, cancellation, authentication failure, connection failure, rate limit, pool exhaustion, or an unavailable provider does not prove absence.
- `body_failed`: a body fetch or integrity failure is distinct from article absence and requires targeted revalidation.
- `unknown`: sampled/partial validation, stale facts, unresolved metadata, ambiguous provider responses, or mixed evidence remain unknown.

**Decision needed:** Which provider responses are authoritative enough to write a negative fact? When should a mixed pool result remain unknown? Should a body failure trigger fallback/repair immediately, or only after targeted STAT revalidation?

**Answer:** _Pending interview._

### I. Repository ownership: `altmount` versus `nntppool`

**Current proposal:** keep provider-pool-specific availability identity, metadata persistence, importer/health writers, and streaming consumption in `altmount`, because they depend on altmount's manifests, metadata, importer, health, and read paths. Consider `nntppool` only for reusable provider-pool primitives that have no altmount-specific manifest or article semantics.

**Decision needed:** Which parts, if any, should move into `nntppool`? Evaluate ownership by API contract, dependency direction, reuse by other consumers, persistence ownership, and whether a change would force a larger cross-repository compatibility surface.

**Answer:** _Pending repository comparison and interview._

## Scope-efficiency rules

- Prefer the smallest PR that establishes one stable contract and one independently testable owner.
- Do not add public configuration, metrics, admin APIs, repair knobs, or abstractions until a real caller needs them.
- Keep provider identity, evidence classification, and invalidation rules explicit before adding writers.
- Avoid duplicating availability semantics in `nntppool` and `altmount`; one repository must own each contract.
- Treat operational tuning changes separately from content-serving identity changes if the interview shows they have different invalidation consequences.
- Preserve first-byte behavior and existing fallback/repair semantics while adding knowledge as an optimization, not a new correctness dependency.

## Safe next steps

1. Monitor PRs #833 and #834; both are open and have not been merged.
2. Monitor PR 1 and PR 2 CI/review; do not merge automatically.
3. Conduct the architecture interview above.
4. Update this file with confirmed decisions and unresolved risks.
5. Only then scout and plan PR 3 (import/health writers), followed by independent plancheck.
6. Keep PR 4 streaming work blocked behind the writer contract and explicit latency/budget decisions.
7. Revisit optional policy/API only after real integration evidence shows it is necessary.
