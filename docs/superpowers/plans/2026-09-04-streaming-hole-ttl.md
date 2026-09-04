# Hole Records with TTL and Provider Fingerprint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A confirmed-missing article stays padded only for 24 hours and only under the provider set that confirmed it (spec phase 8, defect D7); backfills, reposts and newly added providers get a second look.

**Architecture:** `HoleRun` gains `recorded_at` (unix seconds) and `FileMetadata` gains `hole_provider_fingerprint` (SHA-1 of the sorted enabled provider `host:port` list, 8 bytes hex). On open, runs older than `holes.TTL` (24 h) or stored under a different fingerprint are not loaded, so the first read through them re-probes once and re-records. `AddKnownHoles` takes the current fingerprint: a stored fingerprint that differs drops the stored runs first; merged runs are stamped now when they intersect a new run and keep the newest stored stamp otherwise. Legacy records (no stamp, no fingerprint) are treated as unknown, which is the safe direction. Transport failures never create holes today; a test pins that.

**Spec:** `docs/superpowers/specs/2026-09-04-streaming-demand-shaping-design.md` (Phase 8)

## Global Constraints

- No competitor names in code, comments, commits, PRs.
- Conventional Commits; branch `feat/streaming-hole-ttl` from `feat/streaming-body-depth`.
- `make bench-compare BASE=baseline-main`: no regressions (this phase is off the hot path).

---

### Task 1: Proto and metadata

**Files:** `internal/metadata/proto/metadata.proto` (+ regenerate with `make proto`), `internal/metadata/knownholes.go`, `internal/metadata/knownholes_test.go`.

```go
// LiveKnownHoles returns the stored runs still trusted now: stamped within
// holes.TTL and recorded under fingerprint. Unstamped or foreign runs are
// dropped so the next read re-probes them.
func LiveKnownHoles(rows []*metapb.HoleRun, storedFP, fingerprint string, now time.Time) []holes.Run
// AddKnownHoles merges runs into the file's hole map under fingerprint,
// dropping stored runs that were recorded under a different one, and stamps
// the result.
func (ms *MetadataService) AddKnownHoles(virtualPath string, runs []holes.Run, fingerprint string) error
```
`holes.TTL = 24 * time.Hour` in `internal/holes/holes.go`.

- [ ] Tests: fresh stamp under same fingerprint is live; stamp older than TTL is dropped; fingerprint mismatch drops all; unstamped legacy row dropped; `AddKnownHoles` replaces foreign runs, stamps new runs with now and keeps the stored stamp for untouched runs; merge of a new run into an old one takes the new stamp.
- [ ] Commit `feat(metadata): hole runs carry a timestamp and provider fingerprint`.

### Task 2: Provider fingerprint

**Files:** `internal/config/accessors.go`, `internal/config/accessors_test.go`.

```go
// ProviderFingerprint names the set of enabled providers: SHA-1 over the
// sorted "host:port" list, first 8 bytes hex. Hole records are trusted only
// under the fingerprint that recorded them.
func (c *Config) ProviderFingerprint() string
```
- [ ] Tests: order-independent; disabled providers excluded; changes when a provider is added.
- [ ] Commit `feat(config): provider set fingerprint`.

### Task 3: Wire into open and record paths

**Files:** `internal/nzbfilesystem/holes.go` (`holeHooks` load, `holeMetaSnapshot.fingerprint`, `onHole` event), `internal/nzbfilesystem/pad_recorder.go` (`padMetadataStore.AddKnownHoles(path, runs, fingerprint)`, `padEvent.fingerprint`), test fakes implementing `padMetadataStore`, `internal/nzbfilesystem/hole_ttl_test.go`.

`holeHooks()` computes `fp := ""; if mvf.configGetter != nil { fp = mvf.configGetter().ProviderFingerprint() }` and loads `metadata.LiveKnownHoles(mvf.meta.KnownHoles, mvf.meta.HoleProviderFingerprint, fp, time.Now())`.

- [ ] Tests: a hole stamped an hour ago under the current fingerprint pads without a fetch (fetch count 0 for that segment); the same hole stamped 25 h ago is fetched once (430) and padded; a hole under another fingerprint is fetched once; the pad recorder passes the fingerprint through.
- [ ] Reader-level test in `internal/usenet`: a transient error (not `ErrArticleNotFound`) never calls `OnHole`.
- [ ] `go test -race ./internal/metadata/ ./internal/nzbfilesystem/ ./internal/usenet/ ./internal/config/`; commit `feat(nzbfilesystem): trust hole records for 24 h under the recording provider set`.

### Task 4: Bench sanity, PR

- [ ] `make bench-stream && make bench-compare BASE=baseline-main` (expect no change); push; `gh pr create --base feat/streaming-body-depth`.
