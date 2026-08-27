# Media content verification during import and health checks

**Date:** 2026-08-27
**Status:** Approved design, pending implementation plan
**Issue:** https://github.com/javi11/altmount/issues/851

## Problem

AltMount verifies article *availability* (`STAT`) but never proves that the
assembled virtual file actually contains valid media data. A release can pass
segment verification while producing an unplayable stream — e.g. archive
volumes or raw-post segments assembled in the wrong order, or a fake/mislabeled
release. This should be caught two ways:

1. **At import time** — before a queue item is reported successful, so the Arr
   app can blocklist a bad release and search for a replacement.
2. **At health-check time** — so libraries imported before this feature (or
   corrupted afterward, e.g. by upstream article expiry) get flagged and go
   through the existing repair workflow.

Verification must go through the same read path used for playback (so RAR/AES
decryption, archive extraction, etc. all apply transparently), touch at most
one backing NNTP article per file, and distinguish a **definitive** bad result
(wrong/no signature, missing head article) from a **transient** one (timeout,
provider error) — only the former should fail an import or mark a file
corrupted.

## Existing machinery reused

- `internal/nzbfilesystem.NzbFilesystem.OpenFile(ctx, path, flag, perm)`
  (`nzb_filesystem.go`) already opens a virtual file with no FUSE mount
  required — proven by `internal/api/stream_handler.go:179` calling it
  directly from an HTTP handler. Returns an `afero.File` backed by
  `MetadataVirtualFile`, supporting `Read`/`ReadAt`/`Seek`/`Stat`/`Close`.
- `MetadataVirtualFile` already transparently decrypts `metapb.Encryption_AES`
  content before returning bytes to any caller
  (`metadata_remote_file.go:1592-1610`, `:2263-2280`). Both real
  password-protected RAR archives (`archive/rar/processor.go`, unlocked via
  `rardecode.Password`) and obfuscated nested-RAR wrappers
  (`DecryptingFileSystem`) converge on this same field. **No special-casing
  for RAR/AES is needed anywhere in the verification code** — probing through
  `OpenFile` already yields decrypted plaintext.
- Per-call read-ahead override: `metadata_remote_file.go:226` already checks
  `ctx.Value(utils.MaxPrefetchKey).(int)` and honors it when `> 0`, but no
  caller sets this key today. The probe will be the first user, passing `1` to
  cap the read at one backing article.
- `fileinfo/detector.go` already has the hand-rolled magic-byte style to
  follow (`HasRar4Magic`, `HasRar5Magic`, `Has7zMagic`) plus `IsVideoFile` and
  extension-based classification to extend for audio.
- Health status model: `database.HealthStatus` enum
  (`internal/database/models.go:78-87`) already has `Corrupted`/`Degraded`;
  `HealthErrorDetails.ErrorType` (`error_details.go:14`) is a free-text string
  (`"metadata_gap"`, `"missing_segments"` today) — new values slot in without
  a schema change. `checker.go`'s `judgeValidation` (~line 239-272) is the
  exact point where a healthy verdict is currently decided. `worker.go`'s
  `triggerFileRepair`/`prepareUpdateForResult` already route `Corrupted` into
  the repair/Arr-blocklist flow — reused unchanged.
- Import finalize point: `service.go`'s `ProcessItem`/`processNzbItem`
  (~line 355) knows `writtenPaths` before deciding `HandleSuccess` vs
  `HandleFailure`. `handleProcessingFailure` already marks `QueueStatusFailed`,
  which is what surfaces to the Arr app as a failure to blocklist/replace —
  reused unchanged.
- Config pattern: `ImportConfig`/`HealthConfig` in `internal/config/manager.go`
  use `*bool` optional flags with `Get*()` accessors defaulting when nil
  (`accessors.go`). Note: `HealthConfig.VerifyData` already exists but is an
  unrelated, unused, dead field ("verify 1 byte of each segment") — the new
  `VerifyContent` fields are distinct and must not be confused with it.

## New dependency

`github.com/gabriel-vasile/mimetype` — detects container type from magic
bytes for MKV, MP4/MOV, AVI, ASF/WMV, FLV, Ogg, FLAC, MP3, AAC, WAV, AIFF, and
more. It does **not** detect MPEG-TS/M2TS or MPEG-PS/VOB (confirmed against
its `internal/magic/video.go`), so two hand-rolled checks are added alongside
it to cover formats commonly produced by ISO/BDMV remuxes.

## Architecture

### New package: `internal/contentverify`

```go
type Result int
const (
    ContentValid Result = iota
    ContentInvalid
    ContentSegmentMissing
    ContentProbeError // transient
)

type ProbeResult struct {
    Result  Result
    Err     error  // set for ContentProbeError / ContentSegmentMissing
}

// Opener abstracts NzbFilesystem.OpenFile for testability.
type Opener interface {
    OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (afero.File, error)
}

func Probe(ctx context.Context, opener Opener, path string, timeout time.Duration) ProbeResult
```

`Probe`:
1. Applies the configured timeout via `context.WithTimeout`.
2. Sets `ctx = context.WithValue(ctx, utils.MaxPrefetchKey, 1)` to cap
   read-ahead to one article.
3. Calls `opener.OpenFile(ctx, path, os.O_RDONLY, 0)`.
4. Reads up to 512 bytes with `io.ReadFull`, tolerating `io.ErrUnexpectedEOF`
   (small file) as a normal short read, not an error.
5. Classifies the buffer (see below).
6. Maps errors: a missing-first-segment condition (surfaced the same way the
   existing serving path already reports it) → `ContentSegmentMissing`;
   `context.DeadlineExceeded` or a provider/connection error →
   `ContentProbeError`; anything else propagating from `OpenFile`/`Read` is
   also treated as `ContentProbeError` (conservative default — never treat an
   unrecognized failure as definitive).

### Signature classification (`internal/importer/parser/fileinfo`)

New file `mediasignature.go`:

- `IsRecognizedMediaContainer(buf []byte) bool`:
  1. `mimetype.Detect(buf)` → walk its type chain, accept if any ancestor's
     MIME string is in a fixed video/audio whitelist (the full audio+video
     list the library supports, e.g. `video/matroska`, `video/mp4`,
     `video/quicktime`, `video/x-msvideo`, `video/x-ms-asf`, `video/x-flv`,
     `video/webm`, `video/mpeg`, `video/3gpp`, `video/mj2`, `audio/mpeg`,
     `audio/flac`, `audio/ogg`, `audio/aac`, `audio/wav`, `audio/aiff`,
     `audio/basic`, `audio/webm`, `application/ogg`, etc.).
  2. Else `HasMpegTSMagic(buf)`: byte `0x47` at offset 0, and, if enough bytes
     are available, also recurring at offset 188 and 376 (tolerates the
     512-byte probe covering ~2-3 packets).
  3. Else `HasMpegPSMagic(buf)`: `0x00 0x00 0x01 0xBA` pack-header prefix.
  4. Else `false`.

### File eligibility

`IsVerifiableMediaFile(filename string) bool` in the same package: true for
existing `IsVideoFile` extensions **or** a new audio-extension set
(`.mp3 .flac .ogg .aac .m4a .wma .wav .aiff`), false for PAR2, `.nfo`, `.srt`/
subtitle extensions, and sample files (filename contains `sample` case-
insensitively, consistent with scene-release convention). Only eligible files
are probed; everything else is skipped with no article cost.

### Import integration (`internal/importer/service.go`)

After `processNzbItem` returns `writtenPaths` (and before the success/failure
branch), if `cfg.Import.GetVerifyContent()`:

```go
for _, p := range writtenPaths {
    if !fileinfo.IsVerifiableMediaFile(p) { continue }
    switch contentverify.Probe(ctx, s.nzbFilesystem, p, cfg.Import.GetVerifyContentTimeout()).Result {
    case contentverify.ContentInvalid, contentverify.ContentSegmentMissing:
        return definitiveContentError(p) // routed to HandleFailure, same as any other import failure
    case contentverify.ContentProbeError:
        return transientContentError(p, err) // routed through existing retry/backoff, unchanged
    }
}
```

Both error wrappers are plain `error` values (no new import-level enum needed)
— they carry enough message context for logs/UI but flow through the
existing `handleProcessingFailure` / retry machinery unchanged, matching how
other import errors already behave. The distinction that matters (retry vs.
blocklist) is purely which branch returns.

### Health integration (`internal/health/checker.go`)

In `judgeValidation`, right after the existing `result.MissingCount == 0`
check (currently sets `EventTypeFileHealthy` unconditionally), add:

```go
if result.MissingCount == 0 {
    if shouldVerifyContent(cfg, file) { // enabled AND (status == Pending OR manual override requested)
        switch contentverify.Probe(ctx, hc.filesystem, file.Path, cfg.Health.GetVerifyContentTimeout()).Result {
        case contentverify.ContentInvalid, contentverify.ContentSegmentMissing:
            return corruptedEvent(file, "content_invalid" /* or "content_segment_missing" */)
        case contentverify.ContentProbeError:
            // leave status unchanged; next scheduled check retries
        default:
            return healthyEvent(file)
        }
    } else {
        return healthyEvent(file)
    }
}
```

- Only files with `HealthStatus == Pending` are probed on scheduled/automatic
  runs, to avoid repeatedly re-fetching an article for already-`Healthy`
  files. A manual health-check trigger accepts a request-level
  `verify_content` override that forces the probe regardless of current
  status.
- `ContentInvalid`/`ContentSegmentMissing` → `HealthStatusCorrupted` with
  `HealthErrorDetails.ErrorType` set to `"content_invalid"` or
  `"content_segment_missing"`. Flows through the existing
  `triggerFileRepair` path in `worker.go`, unchanged, exactly like
  `"missing_segments"` does today.
- `ContentProbeError` → status left as-is (no downgrade to `Corrupted` or
  `Degraded`); the file is re-evaluated on the next check cycle. This matches
  the issue's requirement that transient infra errors never immediately
  blocklist/corrupt a release.
- The existing `Encryption_AES` exclusion in `loadClassificationInput`
  (`holes.go:103-107`) is about zero-fill padding safety for the
  missing-segment classifier and must **not** be copied into the
  content-verification eligibility check — AES files are fully probeable
  since `OpenFile` decrypts them.

### Config

`internal/config/manager.go`:

```go
type ImportConfig struct {
    // ... existing fields
    VerifyContent        *bool          `yaml:"verify_content,omitempty"`
    VerifyContentTimeout *time.Duration `yaml:"verify_content_timeout,omitempty"`
}

type HealthConfig struct {
    // ... existing fields
    VerifyContent        *bool          `yaml:"verify_content,omitempty"`
    VerifyContentTimeout *time.Duration `yaml:"verify_content_timeout,omitempty"`
}
```

With `GetVerifyContent() bool` (default `false`) and
`GetVerifyContentTimeout() time.Duration` (default e.g. `15s`) accessors in
`accessors.go`, following the existing pattern exactly. These are new,
independent fields — `HealthConfig.VerifyData` (the existing dead
per-segment-byte field) is untouched.

### API / UI

- Config API response/request types (`internal/api/types.go` or wherever
  `ImportConfig`/`HealthConfig` are serialized) gain `verify_content` /
  `verify_content_timeout_seconds` fields.
- Frontend: `ImportConfigSection` and `HealthConfigSection` each get a new
  checkbox ("Verify media content signature") plus a numeric timeout input,
  following the existing boolean-flag UI pattern already used for sibling
  options in those sections.
- Manual health-check trigger (existing API endpoint/UI action) gains an
  optional `verify_content` override checkbox, defaulting to the configured
  value.
- Health event/status display surfaces the new `ErrorType` values
  (`content_invalid`, `content_segment_missing`) with user-facing labels,
  alongside the existing `metadata_gap`/`missing_segments` labels.

## Error handling summary

| Condition | Result | Import | Health |
|---|---|---|---|
| Recognized video/audio signature | `ContentValid` | proceed to success | `HealthStatusHealthy` |
| Wrong/no signature in first 512 bytes | `ContentInvalid` | fail import (routes to `HandleFailure`) | `HealthStatusCorrupted` → repair flow |
| Head article missing | `ContentSegmentMissing` | fail import | `HealthStatusCorrupted` → repair flow |
| Timeout / provider / connection error | `ContentProbeError` | existing retry/backoff (unchanged) | status unchanged, retried next cycle |

## Testing

- `fileinfo`: unit tests for `IsRecognizedMediaContainer` against real magic
  bytes for every whitelisted format plus TS/PS, and against non-media bytes
  (should reject). Unit tests for `IsVerifiableMediaFile` exclusions (PAR2,
  sample, subtitle).
- `contentverify`: `Probe` tests against a fake `Opener` for: valid signature,
  invalid signature, missing-segment error, timeout/provider error, and a
  file shorter than 512 bytes (`io.ErrUnexpectedEOF` tolerance).
- Integration-style test: an AES-encrypted RAR-backed virtual file (reusing
  existing test fixtures for `DecryptingFileSystem`/`archive/rar` if
  available) probed end-to-end through `NzbFilesystem.OpenFile`, confirming
  plaintext is returned with no special-casing.
- Import: a test driving `ProcessItem` with a `writtenPaths` fixture that
  fails the probe, asserting the item lands in `HandleFailure` /
  `QueueStatusFailed`.
- Health: a test driving `judgeValidation` with `MissingCount == 0` and a
  failing probe, asserting `HealthStatusCorrupted` with the right
  `ErrorType`, and a transient-probe-error case asserting status is left
  unchanged.

## Out of scope

- `ffprobe` or any deep-seek validation — explicitly rejected by the issue for
  traffic reasons.
- Validating anything beyond the first backing article of a file.
- Extending the existing `HealthConfig.VerifyData` field or reusing its name —
  it remains a separate, currently-dead feature.
