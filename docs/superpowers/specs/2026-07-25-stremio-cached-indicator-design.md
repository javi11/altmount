# Stremio "Cached/Imported" Stream Indicator

**Date:** 2026-07-25
**Status:** Approved (design)

## Problem

The Stremio addon's stream handler (`handleStremioAddonStream`) returns one stream
option per Prowlarr result. Every option points at the `/play` endpoint, which
downloads and queues the NZB when clicked. Some of those releases are **already
imported** into AltMount and still fresh — clicking them plays instantly — but the
user has no way to tell which ones. This mirrors a gap that debrid addons like
AIOStreams/Torrentio solve by tagging instantly-available results as "cached".

## Goal

Mark each Prowlarr result that is already imported and still fresh with a
`⚡ Cached` badge, and sort those results to the top of the stream list. Uncached
results remain listed and playable. The feature is best-effort: it must never
break stream listing.

## Definition of "cached"

A result is cached if a queue item exists that satisfies **all** of:

- `status = completed`
- `download_id LIKE 'stremio:%'` (originated from the Stremio addon)
- non-empty `storage_path`
- `nzb_path` contains `sanitizeFilename(result.Title) + ".nzb"`
- when `Stremio.NzbTTLHours > 0`: `completed_at` is within the TTL window

This is exactly the short-circuit condition already used by
`handleStremioAddonPlay`. Reusing it guarantees the badge is truthful: a badged
result is one that `/play` will resolve to an instant 302 redirect without a
download.

## Architecture & data flow

1. **Batch lookup (new repo method).** Add
   `Repository.GetCachedStremioQueueItems(ctx) ([]*ImportQueueItem, error)`,
   modeled on `GetExpiredStremioQueueItems`. It selects completed
   `download_id LIKE 'stremio:%'` items with a non-empty `storage_path`, ordered
   by `completed_at DESC`, with a sane cap (`LIMIT 500`). Called **once** per
   stream request — no per-result DB scan.

2. **Handler wiring.** After the existing language/quality filtering in
   `handleStremioAddonStream`, the handler:
   - calls `GetCachedStremioQueueItems`; on error, logs a warning and continues
     with the current behavior (no badges, original order),
   - passes the filtered results + cached items + `NzbTTLHours` to a pure helper,
   - emits the helper's ordered streams.

3. **Pure helper (testable).** Extract a pure function, e.g.

   ```go
   func buildStremioStreamOptions(
       results []prowlarr.NZBResult,
       cached []*database.ImportQueueItem,
       ttlHours int,
       now time.Time,
       ... presentation deps ...,
   ) []streamOption
   ```

   It builds the in-memory set of cached `nzb_path`s (applying the TTL filter in
   Go against `now`), marks each result cached via `strings.Contains`, constructs
   the stream `name`/`title`/`url` exactly as today, and performs a **stable**
   sort placing cached results first while preserving Prowlarr's relative order
   within each group. Taking `now` as a parameter keeps the TTL logic
   deterministically testable.

## Presentation

- Cached results get `⚡ Cached` prepended to the existing badge, e.g.
  `⚡ Cached · AltMount 🇪🇸 4K - La película (2024) [2160p][Esp]`.
- Uncached results are rendered exactly as today.
- Stable sort: cached group first, original order preserved within each group.
- The `url` continues to point at `/play` (unchanged). `/play` already
  short-circuits cached items to a fast redirect, so this avoids duplicating the
  episode-selection and multi-episode-ambiguity logic in `buildStremioStreams`.

## Error handling / degradation

- Repo lookup failure → warn + fall back to current behavior. Never fatal.
- No cached items → helper returns streams in original order with no badges.

## Testing

Table-driven tests for the pure helper:

- cached within TTL → badged and sorted first
- cached but expired (`completed_at` older than TTL) → not badged
- `TTLHours <= 0` → cached regardless of `completed_at` age
- completed item with empty `storage_path` → not treated as cached
- non-Stremio item present in input → ignored (defense-in-depth; repo already
  filters)
- sort stability: two cached + two uncached preserve intra-group order
- empty cache slice → all uncached, original order

Repo method covered by a package-style integration test if the database package
has an existing harness for it.

## Files touched

- `internal/database/repository.go` — new `GetCachedStremioQueueItems` method.
- `internal/api/stremio_addon_handlers.go` — handler wiring + extracted pure
  helper.
- `internal/api/stremio_addon_handlers_test.go` (new or existing) — helper tests.

## Out of scope (YAGNI)

- Linking cached results directly to the media stream URL (bypassing `/play`).
- Any change to `/play`, `buildStremioStreams`, or episode-selection logic.
- Indexing `nzb_path` for the LIKE lookup (batch fetch avoids the need).
