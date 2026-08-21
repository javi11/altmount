# Legacy metadata → v3 store-backed migration

**Date:** 2026-08-21
**Status:** Approved design, pending implementation plan

## Problem

Libraries imported before the v3 shared-`NzbStore` format (commit `c7f73182`) have
`.meta` files carrying inline `segment_data`: one `SegmentData` message per segment,
per file. For a RAR release every virtual file repeats the same outer-volume
segments, so the on-disk cost is O(files × segments).

v3 stores the segments once per release in a zstd-compressed `.nzbz` sidecar and
leaves each `.meta` holding only `segment_refs`/`segment_runs` into that store's flat
segment index. New imports already write v3 (`processor.go:582` →
`WriteFileMetadataAuto`). Existing libraries never convert.

**Goal:** migrate every legacy `.meta` to v3, merging all files of a release onto one
shared `.nzbz`, to reclaim disk space. Triggered manually from the AltMount panel,
with a dry run first.

Explicitly out of scope: there is no distinct "v2" on-disk format (the only magic is
`{0x00,'A','M','3',0x01}`); metas carrying `shared_outer_sources` are just legacy
metas with a dedup layer and are handled by the same path.

## Existing machinery reused

- `MetadataService.WriteFileMetadataV3` (`service.go:263`) already *is* the
  converter: given an inline-segment `FileMetadata`, a `messageID → flatIndex` map and
  a store ref, it converts main + PAR2 + nested segments to refs/runs, sets
  `store_ref`/`source_nzb_path`, writes atomically, and calls `IncStoreRef` once.
- `segDataToRefs` + `splitRefs` (`store.go`) do the ref conversion and run compaction.
- `ExpandSharedOuterSources` (`expand.go`) dissolves the multi-extent dedup.
- `StoreService.WriteStore` / `ReadStore` (`store.go`) persist and verify `.nzbz`.
- `parser.BuildStore` + `nzbparser.Parse` build a faithful store from a real `.nzb`,
  with no `Parser` instance and no NNTP.
- `LibrarySyncWorker` (`internal/health/library_sync.go:65`) is the structural model
  for the worker; `internal/api/server.go:292` for the routes; `useLibrarySync.ts`
  for the hook.

Two facts that make in-place rewriting safe:

- `truncateFilename` is idempotent, so a walked `.meta` path maps back to a
  `virtualPath` that re-derives to the same file — no orphaning.
- `segDataToRefs` writes `decoded_bytes` from each `SegmentData.SegmentSize`, and the
  read path prefers `decoded_bytes` over `NzbSeg.Bytes`, so a synthesized store
  carrying decoded sizes is self-consistent and loses no size information.

## Architecture

New file `internal/metadata/migration.go` defines `MigrationWorker`, modeled on
`LibrarySyncWorker`: `mu`/`running` guard, `cancelFunc`, `progressMu`/`progress`,
`lastResult`. Methods: `Scan`, `DryRun`, `Start`, `Cancel`, `Status`.

### Pass 1 — discover and group

Walk `ms.rootPath` for `*.meta`. Read the first 5 bytes and skip anything with the v3
magic — this is what makes the migration idempotent and resumable. Unmarshal each
legacy meta and derive a group key:

1. primary: `source_nzb_path`
2. fallback when empty/missing: the meta file's parent directory

Yields `map[groupKey][]legacyMeta{metaPath, virtualPath, meta}`.

### Pass 2 — per group, convert

Groups are processed in a stable order (sorted meta path).

**Store provenance chain:**

1. **Faithful store** — if `source_nzb_path` still exists on disk: `nzbparser.Parse`
   it, then `parser.BuildStore(n)`. Real subjects, posters, dates, groups, segment
   numbers and raw byte counts. Before writing anything, pre-check that *every*
   segment id in *every* meta of the group resolves in that index; if any is missing
   (edited or mismatched NZB), abandon the faithful path for this group and fall
   through. The faithful store carries every segment in the original NZB, including
   files no meta references — slightly larger than a synthesized store, still vastly
   smaller than inline segments.
2. **Synthesized store** — otherwise. Walk each meta's segments in order (main
   `segment_data`, then each `par2_files[].segment_data`, then
   `nested_sources[].segments` after `ExpandSharedOuterSources`), appending every
   not-yet-seen message-id and recording `id → flatIndex`. Emit
   `NzbSeg{Id, Number: ordinal, Bytes: SegmentData.SegmentSize}`, one `NzbFileEntry`
   per contributing meta, with `subject` = virtual filename and `groups` =
   `[metadata.migration.default_group]`. One entry per meta keeps each file's segments
   on a contiguous increasing index range, so `splitRefs` collapses them into
   `segment_runs` — where the space win comes from, on top of zstd.

**Then, per group:**

1. Write the store to a fresh path under `<configDir>/.nzbs/_migrated/`, named
   `<sanitized-group-base>-<shortHash>.nzbz`, where `shortHash` is the first 8 hex
   characters of the SHA-256 of the store's segment ids joined in flat order. Read it back to verify (same write-then-verify as `processor.go:582`). On
   either failure, skip the whole group and leave it as v1.
2. Repoint each meta with `WriteFileMetadataV3(ctx, virtualPath, meta, index, storeRef)`.
   Refcounts land at exactly the number of migrated metas, so existing
   deletion/refcount logic needs no changes.

Everything else on the meta is preserved untouched: `known_holes`, `status`,
`clip_boundaries`, AES keys, `release_date`, `nzbdav_id`, `password`/`salt`.

### Why store paths are content-hashed and never overwritten

If a run is cancelled or a file fails mid-group, some metas are v3 and some remain v1.
On re-run the v3 metas are skipped, so a rebuilt store for that group would contain
only the *remaining* segments — a different flat index. Overwriting the original
`.nzbz` would silently corrupt every already-migrated meta pointing at it. A fresh
hashed path per run means a partially-migrated group simply gets a second, smaller
store. Marginally less compact in that rare case, never wrong.

## Correctness and safety

**Core invariant:** for every migrated file, `ReadFileMetadata` after migration returns
`segment_data` byte-identical to before — same ids, `segment_size`, `start_offset`,
`end_offset`, order — and likewise for each PAR2 file and nested source. If that holds,
streaming behaviour cannot change.

**Live-server safety** (the worker runs while serving):

- Store before metas, always: a meta only names a `.nzbz` already written and verified.
- Meta rewrites use `WriteFileMetadata`'s existing atomic tmp+rename, so a reader sees
  either the whole old file or the whole new one.
- An in-flight stream has already resolved its segments into memory; rewriting its meta
  underneath it changes nothing. The next open reads v3 and resolves through the store.
- Cancellation is checked between files and between groups, never mid-file.
- One migration at a time, guarded by the `running` flag.
- A per-file failure (empty segment id, unreadable meta) leaves that file as v1 and is
  counted in the result. Nothing is ever deleted: inline data disappears only because
  the meta is rewritten, and the equivalent data lives in the verified store.
- Pacing: a fixed 100ms sleep between groups so a large library does not saturate disk
  while streams are being served.

## Config

One new field: `metadata.migration.default_group`, default `alt.binaries.misc`. Used
only for synthesized stores. Without it, `nzb.BuildNZB` would render an empty
`<groups>` element and most external clients reject such an NZB. Faithful stores carry
the real groups. Streaming, health checks and PAR2 repair are unaffected either way —
they resolve by message-id.

## API

New `internal/api/metadata_migration_handlers.go`, routes alongside the library-sync
ones:

- `GET  /api/metadata/migration/status` → `{is_running, progress, last_result}`
- `POST /api/metadata/migration/dry-run` → scans and converts *in memory*, writing
  nothing to disk, so numbers are measured rather than estimated: legacy file count, group
  count, faithful-vs-synthesized split, current bytes, projected bytes, savings, and
  the files that would fail
- `POST /api/metadata/migration/start`
- `POST /api/metadata/migration/cancel`

## UI

A "Storage migration" card in the existing `MetadataConfigSection.tsx`, driven by a
`useMetadataMigration` hook modeled on `useLibrarySync.ts`:

- **Idle:** one-line summary ("142 legacy files across 18 releases") + **Dry run**
- **After dry run:** result table (releases, files, current size, projected size,
  savings, files that cannot convert) + **Migrate**
- **Migrate:** confirmation modal stating plainly that `.meta` files are rewritten in
  place and the change is one-way
- **Running:** progress bar (group n of m, current release) + **Cancel**
- **Done:** last-result panel with counts, bytes reclaimed, failures

## Testing

- Round-trip invariant over fixtures for each shape: single file, multi-file, RAR with
  sliced seams, nested RAR, `shared_outer_sources` Blu-ray, PAR2-bearing. Fixture style
  follows `format_v3_test.go` / `segrefs_test.go`.
- Grouping: `source_nzb_path` primary, parent-directory fallback.
- Run compaction actually happens: a plain file collapses to `segment_runs`, not N refs.
- Faithful path: with the source `.nzb` present, groups survive into the store and
  `RegenerateNZB` round-trips through `nzbparser`.
- Faithful pre-check rejects a mismatched NZB and falls back to synthesis.
- Re-run safety: cancel mid-group, re-run, confirm already-v3 metas are skipped, the
  surviving v1 metas get a fresh store, and the first store is untouched.
- Refcounts equal the number of metas pointing at each store.
- Idempotence: a second full run is a no-op.
