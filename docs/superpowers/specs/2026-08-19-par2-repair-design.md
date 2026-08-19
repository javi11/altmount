# PAR2 Repair for AltMount — Design

**Date:** 2026-08-19
**Status:** Approved for planning
**Context:** The "Six ways to stream Usenet" benchmark (2026-08-18) shows zurg as the only
server that repairs a dead article via PAR2: instant zero-fill on first read, a ~3-minute
background Reed–Solomon rebuild, then byte-exact data forever, atomically, surviving
restarts. AltMount today zero-fills instantly but "the bytes never come back." This design
adds background PAR2 repair on top of AltMount's existing hole handling.

## Goals

- Keep today's instant zero-fill answer on the hot read path (no added latency).
- Repair missing articles in the background using the release's PAR2 recovery data,
  streamed from NNTP — never materializing the release on disk or in memory.
- Persist repaired bytes locally; serve them on all later reads, byte-exact.
- Atomic visibility: a reader sees zero-fill or the complete verified payload, never
  partial/garbage data.
- Work for plain files and for RAR/nested/AES releases (whose streams currently fail on a
  missing article rather than zero-fill).

## Non-goals

- Inline (blocking) repair during a read.
- General local content caching (patch store holds repaired articles only).
- SIMD/CGO acceleration (interface allows a ParPar backend later; the network is the
  bottleneck at ~40 MB/s, GF math in pure Go is not).

## Key facts the design relies on

- PAR2 is Reed–Solomon over GF(2^16) across the whole recovery set: repairing k missing
  slices requires reading **every** present input slice once plus k recovery slices.
  Cost of any repair ≈ one full release download. Memory ≈ k × slice_size (accumulators)
  + k×k matrix — never release-sized.
- `NzbStore` in file metadata retains the complete original NZB (every RAR volume's full
  segment list) — the entire recovery set is reachable from metadata alone.
- `Par2FileReference` in metadata retains each `.par2`/`.volXX+YY.par2` file's segments.
- `known_holes` (HoleRun) already persists confirmed-dead segments per file.
- The existing par2 parser reads only `Main`/`FileDesc` packets; `IFSC` (per-slice
  MD5+CRC32) and `RecvSlic` (recovery slice payloads) parsing must be added.
- GF(2^16) arithmetic and the RS coder come from `github.com/akalin/gopar`'s `gf2p16` and
  `rsec16` packages (importable without its file-level layer).

## Architecture

```
 stream hits hole ──────►┌─────────────────────────────┐
 health check degraded ─►│  repair.Queue (per-file      │
 POST /api/.../repair ──►│  dedup, persistent state)    │
                         └────────────┬─────────────────┘
                                      │ max_concurrent_jobs (default 1)
                         ┌────────────▼─────────────────┐
                         │  repair.Job                  │
                         │  plan → sweep → solve →      │
                         │  verify → emit patches       │
                         └────────────┬─────────────────┘
                         ┌────────────▼─────────────────┐
                         │  repair.PatchStore           │
                         │  one blob per repaired       │
                         │  article, keyed by msg-ID    │
                         └────────────┬─────────────────┘
                                      │ read-only lookup
               usenet reader hole branch: patch hit → serve bytes;
               miss → today's zero-fill / fail (hot path untouched)
```

New package: `internal/repair` (queue, planner, job, patch store, solver interface).
Extended package: `internal/importer/parser/par2` (IFSC + RecvSlic packet parsing).

## Components

### par2 parser extension (`internal/importer/parser/par2`)

- Parse `IFSC` packets: per-slice MD5 + CRC32 for each protected file.
- Parse `RecvSlic` packet **headers**: exponent + payload location (par2 segment index +
  byte offset). Recovery payloads are streamed on demand during the solve, never loaded
  wholesale.
- Parse `Main` packet fully (slice size, recovery-set file IDs in order).

### repair.Planner (pure logic)

Inputs: file metadata (`known_holes`, segment maps, `Par2Files`), NzbStore, parsed PAR2
index, and the trigger's failing article (if any). Note `known_holes` is only populated
for hole-eligible (plain video) files — for RAR/AES/nested files the enqueue event must
carry the failing segment/article, and the planner confirms the missing set by STAT-probing
candidate articles across providers before committing to a plan. Output: a repair plan —

- global slice indices missing (dead articles mapped through volume byte offsets),
- articles covering each missing slice and the slice↔article byte mapping,
- which recovery slices to fetch (need ≥ k),
- cap verdict: proceed / unrepairable (policy or math).

Recovery-set membership and ordering are established by matching `FileDesc`
(MD5-16k, MD5, length, filename) against NzbStore entries.

### repair.Job

Executes a plan under a context:

1. Sequential sweep over all recovery-set articles (NzbStore order) fetched via the pool's
   **import lane** (streaming stays prioritized), N concurrent article fetches.
2. Per completed input slice: CRC32 check against IFSC. A present-but-corrupt slice is
   reclassified as missing (k grows → replan; abort if beyond caps). Then fold the slice
   into the k accumulators (GF multiply-accumulate) and discard it.
3. Fetch the chosen recovery slices from par2 volume segments; include in the solve.
4. Invert the k×k matrix, recover missing slices.
5. Verify every recovered slice against its IFSC MD5. Any mismatch → abort, job
   `unrepairable`, nothing stored.
6. Cut recovered volume bytes into article payloads (plan's byte mapping) and write each
   to the PatchStore atomically.

Memory: k × slice_size accumulators, bounded by `max_memory_mb`; a plan exceeding the
budget is `unrepairable` in the prototype (mmap spill is a later optimization).

### repair.PatchStore

- Directory under the metadata root (e.g. `<metadata_root>/patches/`).
- One file per repaired article, named `sha256(message-id)`; content = decoded article
  payload. Plus a small per-release manifest for bookkeeping/pruning.
- Writes: temp file + rename (atomic). Readers either miss (→ zero-fill) or read a
  complete verified payload. This is the atomicity guarantee.
- Patches are regenerable (re-run repair), so pruning is always safe.

### repair.Queue

- Per-file dedup; persistent state in the existing SQLite DB (`repair_jobs`: file, status
  pending/running/repaired/unrepairable, attempts, last_error, timestamps).
- Retry with exponential backoff on transient failure; capped attempts. Survives restarts.
- `max_concurrent_jobs` (default 1).

### Read-path change (single branch)

Where the usenet reader today decides zero-fill/fail for a known/confirmed hole:
ask the PatchStore first (optional lookup func added to `HoleHooks`, wired from
`MetadataVirtualFile`). Hit → serve patched bytes. Miss → existing behavior, unchanged.
No change on the healthy-read hot path.

### Triggers

1. **Playback**: the padRecorder / DataCorruptionError paths additionally enqueue the file
   for repair.
2. **Health check**: a degraded verdict enqueues repair before any ARR involvement; ARR
   replacement remains the fallback when repair is `unrepairable`.
3. **Manual**: `POST /api/files/repair` (standard response envelope) for user-driven and
   test-driven repairs.

## Configuration

```yaml
repair:
  enabled: true
  max_repair_ratio: 0.02   # fraction of file bytes repairable; default = holes cap (2%).
                           # Effective limit is always min(this, PAR2 redundancy available,
                           # memory budget).
  max_memory_mb: 256       # accumulator budget per job
  max_concurrent_jobs: 1
```

## Data flow (one repair)

enqueue → dedup/persist → plan (download smallest .par2 index file only; parse; map dead
articles → slices; check caps & recovery availability) → sweep all input slices from NNTP
(verify CRC, fold, discard) → fetch k recovery slices → matrix solve → MD5-verify recovered
slices → write article patches atomically → mark `repaired`; health flips to healthy with a
`repaired` annotation. `known_holes` is retained — it is what routes reads to the patch
lookup cheaply.

## Error handling

- Insufficient recovery data, par2 articles dead, IFSC verification failure, or memory/
  ratio caps exceeded → `unrepairable`; existing ARR/corruption path proceeds as today.
- Transient NNTP failures: per-article retries (existing machinery) + job-level backoff.
- Crash mid-job: patches already written are atomic and excluded from the missing set on
  replan; the job restarts from plan idempotently.
- Repair never blocks or delays a read; it consumes import-lane connections only.

## Testing

- **Unit**: IFSC/RecvSlic parsing against `testsupport/par2gen` output and real
  `par2cmdline` fixtures; table-driven Planner tests (hole runs × slice sizes × caps ×
  redundancy); GF solve round-trip (generate → delete slices → solve → byte-compare).
- **Integration** (existing fake-NNTP harness): import release → kill article → stream
  (zero-fill) → repair → stream again (md5-exact). Same for a RAR release (fail → repair
  → success).
- **Concurrency**: reads hammering a file while its repair job writes patches must see
  only zeros-or-exact-bytes; run with `-race`.

## Decisions log

- Patch granularity: **article-level** (Option A) — one lookup on the cold hole branch,
  works for all archive/encryption shapes, minimal disk.
- Solver: **pure-Go streaming** with gopar's `gf2p16`/`rsec16`; par2cmdline-turbo/ParPar
  rejected for the prototype (file-based repair requires release-sized temp disk; network
  is the wall-clock bottleneck anyway). Solver sits behind an interface for a later
  ParPar backend.
- Cap policy: **ratio-based** (`max_repair_ratio`), default equal to the holes byte-ratio
  cap; PAR2 redundancy is always the implicit hard ceiling.
