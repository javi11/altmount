# Media content verification during import and health checks — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect assembled-but-unplayable media (wrong archive order, fake releases) by probing the first 512 bytes of eligible video/audio files through the existing serving stack, both during import (fail before reporting success) and during health checks (mark corrupted, trigger repair).

**Architecture:** A new dependency-free-ish `internal/contentverify` package probes one file via an `Opener` interface (satisfied structurally by `*nzbfilesystem.NzbFilesystem`), reading ≤512 bytes with read-ahead capped to one article, classifying the result with `mimetype.Detect` plus two hand-rolled MPEG-TS/PS checks in `internal/importer/parser/fileinfo`. Import (`internal/importer/service.go`) and health (`internal/health/checker.go`) each call `contentverify.Probe` at their existing success/healthy decision points and route `ContentInvalid`/`ContentSegmentMissing` into their existing failure/corrupted/repair paths, while `ContentProbeError` (transient) leaves existing retry behavior untouched. RAR/AES-encrypted content needs no special-casing: `MetadataVirtualFile` already decrypts before `Read` returns bytes.

**Tech Stack:** Go 1.26, `github.com/gabriel-vasile/mimetype` (new dependency), existing `afero.File`/`nzbfilesystem` serving stack, React/TypeScript frontend with DaisyUI.

**Spec:** `docs/superpowers/specs/2026-08-27-media-content-verification-design.md`

## Global Constraints

- Probe reads at most 512 bytes and at most one backing NNTP article per file (`ctx = context.WithValue(ctx, utils.MaxPrefetchKey, 1)`).
- Never call `ffprobe` or seek beyond the first 512 bytes.
- A definitive result (`ContentInvalid`, `ContentSegmentMissing`) fails import / marks health `Corrupted`. A transient result (`ContentProbeError`, e.g. timeout, `context.DeadlineExceeded`, or any error that isn't a confirmed missing-article or a successfully-read-but-unrecognized buffer) must never do either — it flows through existing retry mechanisms unchanged.
- No special-casing for `metapb.Encryption_AES` anywhere in `contentverify` or its callers — the serving stack already decrypts.
- New config fields are `VerifyContent` / `VerifyContentTimeoutSeconds`, never touching the existing dead `HealthConfig.VerifyData`.
- Only files eligible per `fileinfo.IsVerifiableMediaFile` (video or audio extension, minus PAR2/sample/subtitle/nfo) are probed.
- On scheduled/automatic health checks, only probe files whose current `HealthStatus` is `Pending` (fresh imports / newly added), not files being rechecked after repair/degraded/corrupted. A manual recheck can override this via an explicit request flag.

---

### Task 1: Media container signature detection

**Files:**
- Modify: `go.mod`, `go.sum` (add `github.com/gabriel-vasile/mimetype`)
- Create: `internal/importer/parser/fileinfo/mediasignature.go`
- Test: `internal/importer/parser/fileinfo/mediasignature_test.go`

**Interfaces:**
- Produces: `func IsRecognizedMediaContainer(buf []byte) bool` — used by Task 3 (`contentverify.Probe`).

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/gabriel-vasile/mimetype@latest`

Expected: `go.mod`/`go.sum` updated with the new module.

- [ ] **Step 2: Write the failing tests**

```go
// internal/importer/parser/fileinfo/mediasignature_test.go
package fileinfo

import "testing"

func TestIsRecognizedMediaContainer(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"matroska/webm EBML", []byte{0x1A, 0x45, 0xDF, 0xA3, 0, 0, 0, 0}, true},
		{"mp4 ftyp box", append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p'}, make([]byte, 16)...), true},
		{"avi RIFF", append([]byte("RIFF\x00\x00\x00\x00AVI LIST"), make([]byte, 4)...), true},
		{"mp3 frame sync", []byte{0xFF, 0xFB, 0x90, 0x00, 0, 0, 0, 0}, true},
		{"flac", []byte("fLaC\x00\x00\x00\x00"), true},
		{"ogg", []byte("OggS\x00\x02\x00\x00"), true},
		{"mpeg-ts sync bytes at 188 stride", mpegTSFixture(), true},
		{"mpeg-ps pack header", []byte{0x00, 0x00, 0x01, 0xBA, 0x44, 0, 0, 0}, true},
		{"plain text, not media", []byte("this is not a media file, just text padding here"), false},
		{"empty", []byte{}, false},
		{"too short for any signature", []byte{0x1A, 0x45}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRecognizedMediaContainer(tt.data); got != tt.want {
				t.Errorf("IsRecognizedMediaContainer(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// mpegTSFixture builds a buffer with the 0x47 sync byte recurring at the
// 188-byte MPEG-TS packet stride, padded to look like a real capture.
func mpegTSFixture() []byte {
	buf := make([]byte, 512)
	for i := 0; i < len(buf); i += 188 {
		buf[i] = 0x47
	}
	return buf
}

func TestHasMpegTSMagic(t *testing.T) {
	buf := mpegTSFixture()
	if !HasMpegTSMagic(buf) {
		t.Error("expected MPEG-TS sync-byte stride to be detected")
	}
	if HasMpegTSMagic([]byte{0x47, 0, 0}) {
		t.Error("a single sync byte with no stride must not be treated as MPEG-TS")
	}
}

func TestHasMpegPSMagic(t *testing.T) {
	if !HasMpegPSMagic([]byte{0x00, 0x00, 0x01, 0xBA, 0x44}) {
		t.Error("expected MPEG-PS pack header to be detected")
	}
	if HasMpegPSMagic([]byte{0x00, 0x00, 0x01, 0xB3}) {
		t.Error("MPEG sequence header (0xB3) must not be confused with pack header (0xBA)")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/importer/parser/fileinfo/... -run 'TestIsRecognizedMediaContainer|TestHasMpegTSMagic|TestHasMpegPSMagic' -v`
Expected: FAIL — `IsRecognizedMediaContainer`, `HasMpegTSMagic`, `HasMpegPSMagic` undefined.

- [ ] **Step 4: Implement**

```go
// internal/importer/parser/fileinfo/mediasignature.go
package fileinfo

import "github.com/gabriel-vasile/mimetype"

// mediaMimeWhitelist lists every video/audio MIME type mimetype.Detect can
// return (per github.com/gabriel-vasile/mimetype's supported_mimes.md).
// Membership, not mere recognition, is what makes a probe ContentValid —
// a recognized-but-non-media type (e.g. a text/HTML error page) must still
// be rejected.
var mediaMimeWhitelist = map[string]bool{
	"application/ogg":               true,
	"audio/ogg":                     true,
	"audio/flac":                    true,
	"audio/midi":                    true,
	"audio/ape":                     true,
	"audio/musepack":                true,
	"audio/amr":                     true,
	"audio/wav":                     true,
	"audio/aiff":                    true,
	"audio/basic":                   true,
	"audio/aac":                     true,
	"audio/x-unknown":                true,
	"application/vnd.apple.mpegurl": true,
	"application/vnd.rn-realmedia-vbr": true,
	"audio/mpeg":                    true,
	"audio/webm":                    true,
	"audio/qcelp":                   true,
	"video/ogg":                     true,
	"video/mpeg":                    true,
	"video/quicktime":               true,
	"video/mp4":                     true,
	"video/3gpp":                    true,
	"video/3gpp2":                   true,
	"video/x-m4v":                   true,
	"video/mj2":                     true,
	"video/vnd.dvb.file":            true,
	"video/webm":                    true,
	"video/x-msvideo":               true,
	"video/x-flv":                   true,
	"video/matroska":                true,
	"video/x-ms-asf":                true,
	"video/jpm":                     true,
}

// IsRecognizedMediaContainer reports whether buf starts with a signature for
// a known video/audio container. It checks mimetype.Detect's type chain
// against a whitelist (never accepting a recognized-but-non-media type),
// then falls back to hand-rolled MPEG-TS/PS checks that the mimetype
// library does not cover — both are common outputs of ISO/BDMV remuxes.
func IsRecognizedMediaContainer(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}

	mt := mimetype.Detect(buf)
	for m := mt; m != nil; m = m.Parent() {
		if mediaMimeWhitelist[m.String()] {
			return true
		}
	}

	return HasMpegTSMagic(buf) || HasMpegPSMagic(buf)
}

// mpegTSSyncByte is the MPEG-TS packet sync byte, recurring every 188 bytes.
const mpegTSSyncByte = 0x47

// mpegTSPacketSize is the fixed MPEG-TS packet length in bytes.
const mpegTSPacketSize = 188

// HasMpegTSMagic checks for the sync byte 0x47 recurring at the 188-byte
// MPEG-TS packet stride. A single 0x47 byte is common in non-TS data, so at
// least two packets (376 bytes) must be available and agree before this
// returns true.
func HasMpegTSMagic(data []byte) bool {
	if len(data) < 2*mpegTSPacketSize {
		return false
	}
	if data[0] != mpegTSSyncByte {
		return false
	}
	for offset := mpegTSPacketSize; offset < len(data); offset += mpegTSPacketSize {
		if data[offset] != mpegTSSyncByte {
			return false
		}
	}
	return true
}

// mpegPSPackHeader is the MPEG-PS/VOB pack_header start code.
var mpegPSPackHeader = []byte{0x00, 0x00, 0x01, 0xBA}

// HasMpegPSMagic checks for the MPEG-PS/VOB pack_header start code.
func HasMpegPSMagic(data []byte) bool {
	if len(data) < len(mpegPSPackHeader) {
		return false
	}
	for i, b := range mpegPSPackHeader {
		if data[i] != b {
			return false
		}
	}
	return true
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/importer/parser/fileinfo/... -run 'TestIsRecognizedMediaContainer|TestHasMpegTSMagic|TestHasMpegPSMagic' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/importer/parser/fileinfo/mediasignature.go internal/importer/parser/fileinfo/mediasignature_test.go
git commit -m "feat(fileinfo): detect video/audio container signatures"
```

---

### Task 2: Verifiable-media file eligibility

**Files:**
- Modify: `internal/importer/parser/fileinfo/detector.go`
- Test: `internal/importer/parser/fileinfo/detector_test.go` (create if it doesn't already exist — check first)

**Interfaces:**
- Consumes: `videoExtensions` map, `IsVideoFile` (both already in `detector.go`).
- Produces: `func IsVerifiableMediaFile(filename string) bool` — used by Task 5 (import) and Task 6 (health).

- [ ] **Step 1: Check for an existing test file and write the failing test**

Run: `ls internal/importer/parser/fileinfo/detector_test.go 2>/dev/null || echo "none"` first. Then add (to the existing file, or a new one):

```go
func TestIsVerifiableMediaFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Movie.2024.1080p.mkv", true},
		{"episode.s01e01.mp4", true},
		{"soundtrack.flac", true},
		{"song.mp3", true},
		{"audiobook.m4a", true},
		{"archive.par2", false},
		{"movie.par2.vol00+01.par2", false},
		{"Movie.Sample.mkv", false},
		{"sample.avi", false},
		{"subtitle.srt", false},
		{"release.nfo", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsVerifiableMediaFile(tt.name); got != tt.want {
				t.Errorf("IsVerifiableMediaFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/importer/parser/fileinfo/... -run TestIsVerifiableMediaFile -v`
Expected: FAIL — `IsVerifiableMediaFile` undefined.

- [ ] **Step 3: Implement**

Add to `detector.go`:

```go
// audioExtensions lists file extensions eligible for audio content
// verification, in addition to the existing videoExtensions.
var audioExtensions = map[string]bool{
	".mp3": true, ".flac": true, ".ogg": true, ".aac": true,
	".m4a": true, ".wma": true, ".wav": true, ".aiff": true,
}

// samplePattern matches scene-release sample/proof clips, which are
// legitimately short and would false-positive as truncated/invalid content.
var samplePattern = regexp.MustCompile(`(?i)sample`)

// IsVerifiableMediaFile reports whether filename is eligible for content
// signature verification: a video or audio file that is not a sample clip.
// PAR2, subtitle, .nfo, and other non-media sidecars are never eligible.
func IsVerifiableMediaFile(filename string) bool {
	if filename == "" {
		return false
	}
	if samplePattern.MatchString(filename) {
		return false
	}
	ext := strings.ToLower(filepath.Ext(filename))
	return videoExtensions[ext] || audioExtensions[ext]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/importer/parser/fileinfo/... -run TestIsVerifiableMediaFile -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/importer/parser/fileinfo/detector.go internal/importer/parser/fileinfo/detector_test.go
git commit -m "feat(fileinfo): add IsVerifiableMediaFile eligibility check"
```

---

### Task 3: `contentverify` package — `Probe`

**Files:**
- Create: `internal/contentverify/probe.go`
- Test: `internal/contentverify/probe_test.go`

**Interfaces:**
- Consumes: `fileinfo.IsRecognizedMediaContainer` (Task 1), `utils.MaxPrefetchKey` (`internal/utils/path_args.go:23`), `nntppool.ErrArticleNotFound` (`github.com/javi11/nntppool/v4`).
- Produces:
  ```go
  type Result int
  const (
  	ContentValid Result = iota
  	ContentInvalid
  	ContentSegmentMissing
  	ContentProbeError
  )
  type ProbeResult struct {
  	Result Result
  	Err    error
  }
  type Opener interface {
  	OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (afero.File, error)
  }
  func Probe(ctx context.Context, opener Opener, path string, timeout time.Duration) ProbeResult
  ```
  Used by Task 5 (import) and Task 6 (health).

- [ ] **Step 1: Write the failing tests**

```go
// internal/contentverify/probe_test.go
package contentverify

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/javi11/nntppool/v4"
	"github.com/spf13/afero"
)

// fakeFile implements afero.File with only Read/Close exercised by Probe.
type fakeFile struct {
	afero.File // embed to satisfy the interface; only Read/Close are called
	data       []byte
	readErr    error
	pos        int
}

func (f *fakeFile) Read(p []byte) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func (f *fakeFile) Close() error { return nil }

type fakeOpener struct {
	file *fakeFile
	err  error
}

func (o *fakeOpener) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (afero.File, error) {
	if o.err != nil {
		return nil, o.err
	}
	return o.file, nil
}

func TestProbe_ValidSignature(t *testing.T) {
	data := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, make([]byte, 508)...)
	res := Probe(context.Background(), &fakeOpener{file: &fakeFile{data: data}}, "movie.mkv", time.Second)
	if res.Result != ContentValid {
		t.Errorf("got %v, want ContentValid", res.Result)
	}
}

func TestProbe_InvalidSignature(t *testing.T) {
	data := make([]byte, 512) // all zero bytes, no known signature
	res := Probe(context.Background(), &fakeOpener{file: &fakeFile{data: data}}, "movie.mkv", time.Second)
	if res.Result != ContentInvalid {
		t.Errorf("got %v, want ContentInvalid", res.Result)
	}
}

func TestProbe_ShortFileTolerated(t *testing.T) {
	// Shorter than 512 bytes but a valid, complete signature.
	data := []byte("fLaC")
	res := Probe(context.Background(), &fakeOpener{file: &fakeFile{data: data}}, "song.flac", time.Second)
	if res.Result != ContentValid {
		t.Errorf("got %v, want ContentValid for short-but-valid file", res.Result)
	}
}

func TestProbe_MissingSegment(t *testing.T) {
	opener := &fakeOpener{file: &fakeFile{readErr: nntppool.ErrArticleNotFound}}
	res := Probe(context.Background(), opener, "movie.mkv", time.Second)
	if res.Result != ContentSegmentMissing {
		t.Errorf("got %v, want ContentSegmentMissing", res.Result)
	}
	if !errors.Is(res.Err, nntppool.ErrArticleNotFound) {
		t.Errorf("expected wrapped ErrArticleNotFound, got %v", res.Err)
	}
}

func TestProbe_TransientError(t *testing.T) {
	opener := &fakeOpener{file: &fakeFile{readErr: errors.New("connection reset by peer")}}
	res := Probe(context.Background(), opener, "movie.mkv", time.Second)
	if res.Result != ContentProbeError {
		t.Errorf("got %v, want ContentProbeError", res.Result)
	}
}

func TestProbe_OpenFileError(t *testing.T) {
	opener := &fakeOpener{err: errors.New("boom")}
	res := Probe(context.Background(), opener, "movie.mkv", time.Second)
	if res.Result != ContentProbeError {
		t.Errorf("got %v, want ContentProbeError", res.Result)
	}
}

func TestProbe_Timeout(t *testing.T) {
	opener := &fakeOpener{file: &fakeFile{readErr: context.DeadlineExceeded}}
	res := Probe(context.Background(), opener, "movie.mkv", time.Millisecond)
	if res.Result != ContentProbeError {
		t.Errorf("got %v, want ContentProbeError", res.Result)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/contentverify/... -v`
Expected: FAIL — package `contentverify` does not exist.

- [ ] **Step 3: Implement**

```go
// internal/contentverify/probe.go
package contentverify

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/javi11/altmount/internal/importer/parser/fileinfo"
	"github.com/javi11/altmount/internal/utils"
	"github.com/javi11/nntppool/v4"
	"github.com/spf13/afero"
)

// Result classifies the outcome of a content-signature probe.
type Result int

const (
	// ContentValid means a recognized video/audio container signature was found.
	ContentValid Result = iota
	// ContentInvalid means bytes were read but no supported container
	// signature was found — a definitive failure.
	ContentInvalid
	// ContentSegmentMissing means the head article required to read the
	// file's first bytes is confirmed missing — a definitive failure.
	ContentSegmentMissing
	// ContentProbeError means the probe could not complete due to a
	// transient provider, timeout, or connection error — never definitive.
	ContentProbeError
)

// ProbeResult is the outcome of Probe.
type ProbeResult struct {
	Result Result
	Err    error
}

// probeReadSize is the number of bytes read from the start of the file —
// enough to identify every supported container signature.
const probeReadSize = 512

// Opener abstracts *nzbfilesystem.NzbFilesystem.OpenFile for testability.
// nzbfilesystem.NzbFilesystem satisfies this interface structurally.
type Opener interface {
	OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (afero.File, error)
}

// Probe opens path through opener, reads up to the first 512 bytes with
// read-ahead capped to one backing article, and classifies the result.
// Definitive failures (ContentInvalid, ContentSegmentMissing) mean the
// content is confirmed bad; ContentProbeError means the check itself
// failed and must not be treated as a content failure.
func Probe(ctx context.Context, opener Opener, path string, timeout time.Duration) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx = context.WithValue(ctx, utils.MaxPrefetchKey, 1)

	f, err := opener.OpenFile(ctx, path, os.O_RDONLY, 0)
	if err != nil {
		return classifyError(err)
	}
	defer f.Close() //nolint:errcheck // best-effort close on a read-only probe

	buf := make([]byte, probeReadSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return classifyError(err)
	}

	if fileinfo.IsRecognizedMediaContainer(buf[:n]) {
		return ProbeResult{Result: ContentValid}
	}
	return ProbeResult{Result: ContentInvalid, Err: errors.New("no recognized media container signature")}
}

// classifyError maps an Open/Read error to a definitive missing-segment
// result or a transient probe error. A confirmed missing article
// (nntppool.ErrArticleNotFound) is the only definitive case here — anything
// else, including timeouts and unrecognized errors, is conservatively
// treated as transient so it never blocks an import or corrupts a file on
// a flaky connection.
func classifyError(err error) ProbeResult {
	if errors.Is(err, nntppool.ErrArticleNotFound) {
		return ProbeResult{Result: ContentSegmentMissing, Err: err}
	}
	return ProbeResult{Result: ContentProbeError, Err: err}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/contentverify/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/contentverify/probe.go internal/contentverify/probe_test.go
git commit -m "feat(contentverify): add media content signature probe"
```

---

### Task 4: Config fields — import and health `VerifyContent`

**Files:**
- Modify: `internal/config/manager.go` (`ImportConfig` struct ~line 443, `HealthConfig` struct ~line 496)
- Modify: `internal/config/accessors.go`
- Test: `internal/config/accessors_test.go` (check if it exists first; add to it or create)

**Interfaces:**
- Produces: `(*config.Config).GetImportVerifyContent() bool`, `(*config.Config).GetImportVerifyContentTimeout() time.Duration`, `(*config.Config).GetHealthVerifyContent() bool`, `(*config.Config).GetHealthVerifyContentTimeout() time.Duration` — used by Task 5 and Task 6.

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/accessors_test.go (add these test functions)
package config

import (
	"testing"
	"time"
)

func TestGetImportVerifyContent(t *testing.T) {
	c := &Config{}
	if c.GetImportVerifyContent() {
		t.Error("expected false by default")
	}
	enabled := true
	c.Import.VerifyContent = &enabled
	if !c.GetImportVerifyContent() {
		t.Error("expected true when set")
	}
}

func TestGetImportVerifyContentTimeout(t *testing.T) {
	c := &Config{}
	if got := c.GetImportVerifyContentTimeout(); got != 15*time.Second {
		t.Errorf("got %v, want 15s default", got)
	}
	secs := 5
	c.Import.VerifyContentTimeoutSeconds = &secs
	if got := c.GetImportVerifyContentTimeout(); got != 5*time.Second {
		t.Errorf("got %v, want 5s", got)
	}
}

func TestGetHealthVerifyContent(t *testing.T) {
	c := &Config{}
	if c.GetHealthVerifyContent() {
		t.Error("expected false by default")
	}
	enabled := true
	c.Health.VerifyContent = &enabled
	if !c.GetHealthVerifyContent() {
		t.Error("expected true when set")
	}
}

func TestGetHealthVerifyContentTimeout(t *testing.T) {
	c := &Config{}
	if got := c.GetHealthVerifyContentTimeout(); got != 15*time.Second {
		t.Errorf("got %v, want 15s default", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -run 'TestGetImportVerifyContent|TestGetHealthVerifyContent' -v`
Expected: FAIL — fields/methods undefined.

- [ ] **Step 3: Implement**

In `internal/config/manager.go`, add to `ImportConfig` (after `DamagePolicy`):

```go
	// VerifyContent, when true, probes each eligible video/audio file's
	// first bytes through the serving stack after import and fails the
	// import if no recognized media container signature is found.
	VerifyContent               *bool `yaml:"verify_content" mapstructure:"verify_content" json:"verify_content,omitempty"`
	VerifyContentTimeoutSeconds *int  `yaml:"verify_content_timeout_seconds" mapstructure:"verify_content_timeout_seconds" json:"verify_content_timeout_seconds,omitempty"`
```

Add to `HealthConfig` (after `CorruptionAction`), as new, distinct fields — do not touch `VerifyData`:

```go
	// VerifyContent, when true, probes each eligible video/audio file's
	// first bytes through the serving stack during a health check and
	// marks the file corrupted if no recognized media container signature
	// is found. Distinct from the unrelated, unused VerifyData field above.
	VerifyContent               *bool `yaml:"verify_content" mapstructure:"verify_content" json:"verify_content,omitempty"`
	VerifyContentTimeoutSeconds *int  `yaml:"verify_content_timeout_seconds" mapstructure:"verify_content_timeout_seconds" json:"verify_content_timeout_seconds,omitempty"`
```

In `internal/config/accessors.go`, add:

```go
// GetImportVerifyContent returns whether imported media files should be
// probed for a valid container signature before the import is reported
// successful.
func (c *Config) GetImportVerifyContent() bool {
	if c.Import.VerifyContent == nil {
		return false
	}
	return *c.Import.VerifyContent
}

// GetImportVerifyContentTimeout returns the per-file content probe timeout
// for import verification, defaulting to 15 seconds.
func (c *Config) GetImportVerifyContentTimeout() time.Duration {
	if c.Import.VerifyContentTimeoutSeconds == nil || *c.Import.VerifyContentTimeoutSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(*c.Import.VerifyContentTimeoutSeconds) * time.Second
}

// GetHealthVerifyContent returns whether health checks should probe media
// files for a valid container signature.
func (c *Config) GetHealthVerifyContent() bool {
	if c.Health.VerifyContent == nil {
		return false
	}
	return *c.Health.VerifyContent
}

// GetHealthVerifyContentTimeout returns the per-file content probe timeout
// for health check verification, defaulting to 15 seconds.
func (c *Config) GetHealthVerifyContentTimeout() time.Duration {
	if c.Health.VerifyContentTimeoutSeconds == nil || *c.Health.VerifyContentTimeoutSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(*c.Health.VerifyContentTimeoutSeconds) * time.Second
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -run 'TestGetImportVerifyContent|TestGetHealthVerifyContent' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/manager.go internal/config/accessors.go internal/config/accessors_test.go
git commit -m "feat(config): add import and health verify_content options"
```

---

### Task 5: Import integration

**Files:**
- Modify: `internal/importer/service.go` (`Service` struct ~line 174, `NewService` ~line 216, `processNzbItem` ~line 826-858)
- Modify: `cmd/altmount/cmd/setup.go` (`initializeImporter`, ~line 65-100)
- Modify: `cmd/altmount/cmd/serve.go` (wiring, ~line 120-140)
- Test: `internal/importer/service_test.go` (check for existing file first)

**Interfaces:**
- Consumes: `contentverify.Probe`, `contentverify.Opener`, `fileinfo.IsVerifiableMediaFile`, `(*config.Config).GetImportVerifyContent/GetImportVerifyContentTimeout`.
- Produces: `(*Service).SetContentVerifyFilesystem(fs contentverify.Opener)` — called once from `cmd/altmount/cmd/serve.go` after the real `NzbFilesystem` is constructed.

- [ ] **Step 1: Write the failing test**

```go
// internal/importer/service_test.go (add; adjust package-level test helpers
// to match whatever fakes/harness the existing tests in this package already
// use for *database.ImportQueueItem and s.processor — the assertion below is
// the behavior under test regardless of harness specifics.)
func TestProcessNzbItem_ContentVerificationFailure(t *testing.T) {
	svc := newTestServiceWithVerifyContentEnabled(t) // test helper: builds a
	// *Service with a stub processor.ProcessNzbFile that returns one
	// writtenPath (a fake .mkv), and a fake contentverify.Opener whose
	// OpenFile returns 512 zero bytes (no recognized signature).

	item := &database.ImportQueueItem{ID: 1, NzbPath: "test.nzb"}
	_, writtenPaths, err := svc.processNzbItem(context.Background(), item)

	if err == nil {
		t.Fatal("expected an error when content verification fails")
	}
	if len(writtenPaths) != 1 {
		t.Errorf("expected writtenPaths to still be returned for cleanup, got %v", writtenPaths)
	}
}
```

Note: if the existing `internal/importer` test suite has no harness for stubbing `s.processor`/`s.configGetter` cheaply, write this as a smaller, package-internal unit test against a new helper function extracted in Step 3 (`verifyWrittenContent`) instead of the full `processNzbItem` — the important behavior to lock down is "content verification failure produces a non-nil error", not the full processing pipeline.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/importer/... -run TestProcessNzbItem_ContentVerificationFailure -v`
Expected: FAIL (compile error or assertion failure, since the check doesn't exist yet).

- [ ] **Step 3: Implement**

In `internal/importer/service.go`, add to the `Service` struct (~line 191, alongside `poolManager`):

```go
	contentVerifyFS contentverify.Opener // Optional: real NzbFilesystem, set post-construction to break the init-order cycle with nzbfilesystem
```

Add the setter (near `SetArrsService`, ~line 521), following its exact pattern:

```go
// SetContentVerifyFilesystem wires the real serving-stack filesystem used to
// probe imported media files' content signatures. Called once after the
// NzbFilesystem singleton is constructed (see cmd/altmount/cmd/serve.go),
// mirroring SetArrsService's late-binding to avoid an import-time dependency
// cycle between importer and nzbfilesystem.
func (s *Service) SetContentVerifyFilesystem(fs contentverify.Opener) {
	s.mu.Lock()
	s.contentVerifyFS = fs
	s.mu.Unlock()
}
```

Add a helper and wire it into `processNzbItem`'s return (~line 826-858), replacing the bare final `return` with:

```go
func (s *Service) processNzbItem(ctx context.Context, item *database.ImportQueueItem) (string, []string, error) {
	// ... existing body unchanged up to the final return ...

	resultPath, writtenPaths, err := s.processor.ProcessNzbFile(ctx, item.NzbPath, basePath, int(item.ID), allowedExtensionsOverride, &virtualDir, extractedFiles, item.Category, item.Metadata, item.DownloadID)
	if err != nil {
		return resultPath, writtenPaths, err
	}

	if verifyErr := s.verifyWrittenContent(ctx, writtenPaths); verifyErr != nil {
		return resultPath, writtenPaths, verifyErr
	}

	return resultPath, writtenPaths, nil
}

// verifyWrittenContent probes every eligible video/audio file among
// writtenPaths for a recognized media container signature, when
// import.verify_content is enabled. A definitive failure (bad signature or
// confirmed-missing head article) is returned as an error so the caller
// routes the item to HandleFailure exactly like any other import error,
// letting the Arr app blocklist the release. A transient probe error is
// likewise returned as an error — it flows through the same existing
// retry/backoff path as any other transient import failure; no separate
// mechanism is introduced.
func (s *Service) verifyWrittenContent(ctx context.Context, writtenPaths []string) error {
	cfg := s.configGetter()
	if cfg == nil || !cfg.GetImportVerifyContent() || s.contentVerifyFS == nil {
		return nil
	}

	timeout := cfg.GetImportVerifyContentTimeout()
	for _, path := range writtenPaths {
		if !fileinfo.IsVerifiableMediaFile(path) {
			continue
		}

		result := contentverify.Probe(ctx, s.contentVerifyFS, path, timeout)
		switch result.Result {
		case contentverify.ContentValid:
			continue
		case contentverify.ContentInvalid:
			return fmt.Errorf("content verification failed for %q: no recognized media container signature: %w", path, result.Err)
		case contentverify.ContentSegmentMissing:
			return fmt.Errorf("content verification failed for %q: head article missing: %w", path, result.Err)
		case contentverify.ContentProbeError:
			return fmt.Errorf("content verification could not complete for %q: %w", path, result.Err)
		}
	}
	return nil
}
```

Add the two new imports (`"github.com/javi11/altmount/internal/contentverify"`, `"github.com/javi11/altmount/internal/importer/parser/fileinfo"`) to `service.go`'s import block — check `fileinfo` isn't already imported under a different alias first.

In `cmd/altmount/cmd/setup.go`, no signature change is needed for `initializeImporter`/`NewService` — wiring happens post-construction in `serve.go`.

In `cmd/altmount/cmd/serve.go`, immediately after `fs := initializeFilesystem(...)` (~line 140), add:

```go
	importerService.SetContentVerifyFilesystem(fs)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/importer/... -run TestProcessNzbItem_ContentVerificationFailure -v`
Expected: PASS

Then run the full importer package suite to check for regressions: `go test ./internal/importer/... -v`

- [ ] **Step 5: Build the whole project to catch wiring errors**

Run: `go build ./...`
Expected: success (confirms `serve.go`'s new call and the new imports compile).

- [ ] **Step 6: Commit**

```bash
git add internal/importer/service.go cmd/altmount/cmd/serve.go
git commit -m "feat(importer): verify media content signature before reporting import success"
```

---

### Task 6: Health check integration

**Files:**
- Modify: `internal/health/checker.go` (`CheckOptions` ~line 46, `HealthChecker` struct ~line 51, `NewHealthChecker` ~line 60, `preparedCheck` ~line 100, `prepareCheck`, `judgeValidation` ~line 233, `CheckFilesBatch` ~line 309)
- Modify: `internal/health/worker.go` (~line 712-733 single-file path, ~line 848-883 batch path)
- Modify: `cmd/altmount/cmd/setup.go` (`startHealthWorker`, ~line 381-402)
- Modify: `cmd/altmount/cmd/serve.go` (pass `fs` into `startHealthWorker`, ~line 200)
- Test: `internal/health/checker_test.go` (check for existing file first)

**Interfaces:**
- Consumes: `contentverify.Probe`, `contentverify.Opener`, `fileinfo.IsVerifiableMediaFile`, `(*config.Config).GetHealthVerifyContent/GetHealthVerifyContentTimeout`, `database.HealthErrorDetails`.
- Produces: `NewHealthChecker(..., contentVerifyFS contentverify.Opener)` (new trailing param), `CheckOptions.VerifyContentOverride *bool`, `CheckFilesBatch(ctx, filePaths, statuses []database.HealthStatus, opts ...CheckOptions)` (new `statuses` param) — the batch path's per-file gate.

- [ ] **Step 1: Write the failing tests**

```go
// internal/health/checker_test.go (add; use whatever fake poolManager /
// metadataService harness the existing checker tests in this package
// already build — the two behaviors under test are independent of that
// harness's specifics)

func TestJudgeValidation_ContentInvalid(t *testing.T) {
	hc := newTestHealthCheckerWithVerifyContentEnabled(t, &fakeContentOpener{
		data: make([]byte, 512), // no recognized signature
	})
	prep := preparedCheck{filePath: "/movie.mkv", currentStatus: database.HealthStatusPending}
	result := usenetValidationResultAllPresent(t) // helper: MissingCount: 0

	event := hc.judgeValidation(context.Background(), prep, result, nil)

	if event.Status != database.HealthStatusCorrupted {
		t.Errorf("got status %v, want Corrupted", event.Status)
	}
	if !strings.Contains(*event.Details, "content_invalid") {
		t.Errorf("expected content_invalid in details, got %v", event.Details)
	}
}

func TestJudgeValidation_ContentProbeErrorLeavesStatusUnchanged(t *testing.T) {
	hc := newTestHealthCheckerWithVerifyContentEnabled(t, &fakeContentOpener{
		err: errors.New("connection reset"),
	})
	prep := preparedCheck{filePath: "/movie.mkv", currentStatus: database.HealthStatusPending}
	result := usenetValidationResultAllPresent(t)

	event := hc.judgeValidation(context.Background(), prep, result, nil)

	if event.Type != EventTypeFileHealthy {
		t.Errorf("got event type %v, want EventTypeFileHealthy (transient probe error must not corrupt)", event.Type)
	}
}

func TestJudgeValidation_SkipsProbeWhenNotPending(t *testing.T) {
	hc := newTestHealthCheckerWithVerifyContentEnabled(t, &fakeContentOpener{
		data: make([]byte, 512), // would fail if probed
	})
	prep := preparedCheck{filePath: "/movie.mkv", currentStatus: database.HealthStatusDegraded}
	result := usenetValidationResultAllPresent(t)

	event := hc.judgeValidation(context.Background(), prep, result, nil)

	if event.Type != EventTypeFileHealthy {
		t.Errorf("expected a Degraded-status recheck to skip content probing entirely, got %v", event.Type)
	}
}
```

Add matching helpers (`fakeContentOpener` implementing `contentverify.Opener`, `newTestHealthCheckerWithVerifyContentEnabled`, `usenetValidationResultAllPresent`) alongside whatever fakes the existing `checker_test.go` file already defines for `poolManager`/`configGetter`/`metadataService` — reuse them rather than duplicating.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/health/... -run TestJudgeValidation -v`
Expected: FAIL — `currentStatus` field and content-verify behavior don't exist yet.

- [ ] **Step 3: Implement**

In `internal/health/checker.go`:

```go
type CheckOptions struct {
	ForceFullCheck bool
	// VerifyContentOverride, when non-nil, forces (true) or disables
	// (false) content verification for this check regardless of the
	// file's current status or the configured default. Used by the
	// manual recheck API to let a user force a re-probe.
	VerifyContentOverride *bool
}

type HealthChecker struct {
	healthRepo      *database.HealthRepository
	metadataService *metadata.MetadataService
	poolManager     pool.Manager
	configGetter    config.ConfigGetter
	rcloneClient    rclonecli.RcloneRcClient
	contentVerifyFS contentverify.Opener // Optional: real NzbFilesystem for content probing
}

func NewHealthChecker(
	healthRepo *database.HealthRepository,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	configGetter config.ConfigGetter,
	rcloneClient rclonecli.RcloneRcClient,
	contentVerifyFS contentverify.Opener,
) *HealthChecker {
	return &HealthChecker{
		healthRepo:      healthRepo,
		metadataService: metadataService,
		poolManager:     poolManager,
		configGetter:    configGetter,
		rcloneClient:    rcloneClient,
		contentVerifyFS: contentVerifyFS,
	}
}
```

Extend `preparedCheck` (~line 100):

```go
type preparedCheck struct {
	filePath      string
	sourceNzbPath string
	sampledIDs    []string
	earlyEvent    *HealthEvent
	totalSegments int
	// currentStatus is the file's HealthStatus before this check started,
	// used to gate content verification to first-time (Pending) checks
	// unless verifyContentOverride forces it either way.
	currentStatus         database.HealthStatus
	verifyContentOverride *bool
}
```

In `prepareCheck` (the function building `preparedCheck`), thread the two new fields from `opts` right where `prep` is first constructed:

```go
	if len(opts) > 0 {
		prep.verifyContentOverride = opts[0].VerifyContentOverride
	}
```

(`currentStatus` is set by the caller after `prepareCheck` returns — see the `CheckFilesBatch`/`CheckFile` changes below — since `prepareCheck` has no access to the health-status row itself.)

Add the decision helper and wire it into `judgeValidation` (~line 233), replacing the unconditional healthy branch:

```go
// shouldVerifyContent decides whether judgeValidation should run a content
// probe for this file: explicit override wins; otherwise only a first-time
// (Pending) check is probed, so repeated repair-recheck cycles on an
// already-flagged file don't re-probe on every pass.
func (hc *HealthChecker) shouldVerifyContent(prep preparedCheck) bool {
	if prep.verifyContentOverride != nil {
		return *prep.verifyContentOverride
	}
	if hc.contentVerifyFS == nil || !hc.configGetter().GetHealthVerifyContent() {
		return false
	}
	return prep.currentStatus == database.HealthStatusPending
}

func (hc *HealthChecker) judgeValidation(ctx context.Context, prep preparedCheck, result usenet.ValidationResult, valErr error) HealthEvent {
	event := baseResultEvent(prep.filePath, prep.sourceNzbPath)

	if valErr != nil {
		event.Type = EventTypeCheckFailed
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("failed to validate segments: %w", valErr)
		return event
	}

	if result.MissingCount > 0 {
		// ... existing missing-segment branch, unchanged ...
	}

	if hc.shouldVerifyContent(prep) {
		if verified := hc.judgeContentVerification(ctx, prep); verified != nil {
			return *verified
		}
	}

	event.Type = EventTypeFileHealthy
	return event
}

// judgeContentVerification probes prep.filePath's content signature and, if
// the result is definitive, returns a Corrupted event. A nil return means
// either verification is not eligible for this file (not a verifiable media
// type), passed, or failed only transiently — in all three cases the caller
// proceeds to the normal healthy branch, since a transient probe error must
// never mark a file corrupted.
func (hc *HealthChecker) judgeContentVerification(ctx context.Context, prep preparedCheck) *HealthEvent {
	if !fileinfo.IsVerifiableMediaFile(prep.filePath) {
		return nil
	}

	cfg := hc.configGetter()
	result := contentverify.Probe(ctx, hc.contentVerifyFS, prep.filePath, cfg.GetHealthVerifyContentTimeout())

	var errType string
	switch result.Result {
	case contentverify.ContentValid, contentverify.ContentProbeError:
		return nil
	case contentverify.ContentInvalid:
		errType = "content_invalid"
	case contentverify.ContentSegmentMissing:
		errType = "content_segment_missing"
	default:
		return nil
	}

	event := baseResultEvent(prep.filePath, prep.sourceNzbPath)
	event.Type = EventTypeFileCorrupted
	event.Status = database.HealthStatusCorrupted
	event.Error = fmt.Errorf("content verification failed: %s", errType)
	details := database.HealthErrorDetails{ErrorType: errType, Message: result.Err.Error()}
	event.Details = details.Marshal()
	return &event
}
```

Update `CheckFile` (~line 264) to fill in `currentStatus` — it already has no direct access to `*database.FileHealth`, so thread it via a new field the caller sets on `CheckOptions` instead of `preparedCheck` (simpler than touching `prepareCheck`'s signature): add `CurrentStatus database.HealthStatus` to `CheckOptions`, and in `prepareCheck`, copy `prep.currentStatus = opts[0].CurrentStatus` alongside the `verifyContentOverride` line above.

Update `CheckFilesBatch` (~line 309) to accept a per-file status slice, since the shared `opts ...CheckOptions` can't vary per file:

```go
func (hc *HealthChecker) CheckFilesBatch(ctx context.Context, filePaths []string, statuses []database.HealthStatus, opts ...CheckOptions) []HealthEvent {
	if len(filePaths) == 0 {
		return nil
	}

	preps := make([]preparedCheck, len(filePaths))
	pl := concpool.New().WithMaxGoroutines(min(len(filePaths), prepareConcurrency))
	for i, filePath := range filePaths {
		i, filePath := i, filePath
		pl.Go(func() {
			preps[i] = hc.prepareCheck(ctx, filePath, opts...)
			if i < len(statuses) {
				preps[i].currentStatus = statuses[i]
			}
		})
	}
	pl.Wait()

	// ... rest unchanged ...
```

In `internal/health/worker.go`:

- At the single-file path (~line 712-733), where `fh, err := hw.healthRepo.GetFileHealth(ctx, filePath)` is already loaded, change:

  ```go
  opts := CheckOptions{}
  ```

  to:

  ```go
  opts := CheckOptions{CurrentStatus: fh.Status}
  ```

- At the batch path (~line 848-883), build the aligned `statuses` slice from the already-in-memory `unhealthyFiles` (captured before `SetFilesCheckingBulk` overwrites the DB row — the in-memory `fh.Status` still reflects the pre-check value):

  ```go
  paths := make([]string, len(unhealthyFiles))
  statuses := make([]database.HealthStatus, len(unhealthyFiles))
  for i, fh := range unhealthyFiles {
  	paths[i] = fh.FilePath
  	statuses[i] = fh.Status
  }
  events := hw.healthChecker.CheckFilesBatch(ctx, paths, statuses)
  ```

In `cmd/altmount/cmd/setup.go`, add a `contentVerifyFS contentverify.Opener` parameter to `startHealthWorker` (~line 381) and pass it through to `health.NewHealthChecker` (~line 396):

```go
func startHealthWorker(
	ctx context.Context,
	cfg *config.Config,
	healthRepo *database.HealthRepository,
	poolManager pool.Manager,
	configManager *config.Manager,
	rcloneClient rclonecli.RcloneRcClient,
	arrsService *arrs.Service,
	importerService importer.ImportService,
	broadcaster *progress.ProgressBroadcaster,
	contentVerifyFS contentverify.Opener,
) (*health.HealthWorker, *health.LibrarySyncWorker, error) {
	metadataService := metadata.NewMetadataService(cfg.Metadata.RootPath)

	healthChecker := health.NewHealthChecker(
		healthRepo,
		metadataService,
		poolManager,
		configManager.GetConfigGetter(),
		rcloneClient,
		contentVerifyFS,
	)
	// ... rest unchanged ...
```

In `cmd/altmount/cmd/serve.go` (~line 200), pass the already-constructed `fs`:

```go
	healthWorker, librarySyncWorker, err := startHealthWorker(ctx, cfg, repos.HealthRepo, poolManager, configManager, rcloneRCClient, arrsService, importerService, progressBroadcaster, fs)
```

`NewHealthChecker`'s new trailing parameter breaks three existing direct callers — `internal/health/checker_batch_test.go`, `internal/health/holes_test.go`, `internal/health/repair_e2e_test.go`. Update each call site to pass `nil` (content verification simply stays disabled for those tests, since `hc.contentVerifyFS == nil` short-circuits `shouldVerifyContent`) unless the specific test is about content verification itself.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/health/... -run TestJudgeValidation -v`
Expected: PASS

Then: `go test ./internal/health/... -v` to check for regressions from the `NewHealthChecker`/`CheckFilesBatch` signature changes.

- [ ] **Step 5: Build the whole project**

Run: `go build ./...`
Expected: success.

- [ ] **Step 6: Commit**

```bash
git add internal/health/checker.go internal/health/worker.go cmd/altmount/cmd/setup.go cmd/altmount/cmd/serve.go
git commit -m "feat(health): verify media content signature on first health check"
```

---

### Task 7: Manual recheck content-verify override

**Files:**
- Modify: `internal/health/worker.go` (`PerformBackgroundCheck` ~line 350, `performDirectCheck` ~line 698)
- Modify: `internal/api/health_handlers.go` (~line 920-970, the manual recheck handler)
- Test: `internal/health/worker_test.go` (check for existing file first)

**Interfaces:**
- Consumes: `CheckOptions.VerifyContentOverride` (Task 6).
- Produces: `PerformBackgroundCheck(ctx, filePath string, verifyContentOverride *bool) error` (signature change; single existing caller updated in the same task).

- [ ] **Step 1: Write the failing test**

```go
// internal/health/worker_test.go (add, using whatever fake healthChecker /
// healthRepo the existing worker tests already build)
func TestPerformBackgroundCheck_ThreadsVerifyContentOverride(t *testing.T) {
	fakeChecker := &fakeHealthChecker{} // records the CheckOptions it was called with
	hw := newTestHealthWorker(t, fakeChecker)

	forceTrue := true
	err := hw.PerformBackgroundCheck(context.Background(), "/movie.mkv", &forceTrue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fakeChecker.lastOpts.VerifyContentOverride == nil || !*fakeChecker.lastOpts.VerifyContentOverride {
		t.Error("expected VerifyContentOverride=true to be threaded through to the checker")
	}
}
```

Add a `fakeHealthChecker` (or extend whatever fake is already used) that records the last `CheckOptions` it received, matching the `HealthChecker` interface `performDirectCheck` actually calls against (check whether `worker.go` calls a concrete `*health.HealthChecker` or an interface — if concrete, this test may need to live at a level where `performDirectCheck`'s behavior is exercised indirectly; adjust to the existing test harness rather than introducing a new interface just for this test).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/health/... -run TestPerformBackgroundCheck_ThreadsVerifyContentOverride -v`
Expected: FAIL — signature mismatch.

- [ ] **Step 3: Implement**

In `internal/health/worker.go`:

```go
func (hw *HealthWorker) PerformBackgroundCheck(ctx context.Context, filePath string, verifyContentOverride *bool) error {
	// ... existing body unchanged, except passing verifyContentOverride through
	// to performDirectCheck ...
	checkErr := hw.performDirectCheck(ctx, filePath, verifyContentOverride)
	// ...
}

func (hw *HealthWorker) performDirectCheck(ctx context.Context, filePath string, verifyContentOverride *bool) error {
	// ... existing body unchanged up to building opts ...
	opts := CheckOptions{CurrentStatus: fh.Status, VerifyContentOverride: verifyContentOverride}
	event := hw.healthChecker.CheckFile(checkCtx, filePath, opts)
	// ... rest unchanged ...
}
```

In `internal/api/health_handlers.go` (~line 920-968), parse an optional `verify_content` field from the request body before the existing `PerformBackgroundCheck` call:

```go
	var body struct {
		VerifyContent *bool `json:"verify_content"`
	}
	_ = c.BodyParser(&body) // optional body; ignore parse errors on an empty/absent body

	// ... existing checks unchanged ...

	err = s.healthWorker.PerformBackgroundCheck(context.Background(), item.FilePath, body.VerifyContent)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/health/... -v && go test ./internal/api/... -run Health -v`
Expected: PASS

- [ ] **Step 5: Build**

Run: `go build ./...`

- [ ] **Step 6: Commit**

```bash
git add internal/health/worker.go internal/api/health_handlers.go internal/health/worker_test.go
git commit -m "feat(health): allow manual recheck to override content verification"
```

---

### Task 8: Config API and frontend types

**Files:**
- Modify: `frontend/src/types/config.ts` (`ImportConfig` interface ~line 230, `HealthConfig` interface ~line 110)

**Interfaces:**
- Consumes: nothing new (config API already serializes `*config.Config` directly via JSON tags — no Go-side API mapping change needed).
- Produces: `ImportConfig.verify_content?`, `ImportConfig.verify_content_timeout_seconds?`, `HealthConfig.verify_content?`, `HealthConfig.verify_content_timeout_seconds?` — used by Task 9.

- [ ] **Step 1: Update the types**

In `frontend/src/types/config.ts`, add to `ImportConfig` (~line 246, after `history_retention_days`):

```typescript
	verify_content?: boolean; // Probe each media file's header for a valid container signature before reporting import success
	verify_content_timeout_seconds?: number; // Per-file content probe timeout (default 15s)
```

Add to `HealthConfig` (~line 134, after `corruption_action`), as new fields distinct from `verify_data`:

```typescript
	verify_content?: boolean; // Probe each media file's header for a valid container signature during health checks
	verify_content_timeout_seconds?: number; // Per-file content probe timeout (default 15s)
```

- [ ] **Step 2: Verify the frontend still type-checks**

Run: `cd frontend && bun run check`
Expected: success (adding optional fields to an interface cannot break existing usages).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/types/config.ts
git commit -m "feat(frontend): add verify_content config type fields"
```

---

### Task 9: Frontend config UI

**Files:**
- Modify: `frontend/src/components/config/WorkersConfigSection.tsx` (import config UI lives here — verify by checking where `filter_sample_files` renders, ~line 230-245)
- Modify: `frontend/src/components/config/HealthConfigSection.tsx` (~line 495-515, alongside the existing `verify_data`/"Ghost File Detection" checkbox)

**Interfaces:**
- Consumes: `formData.verify_content`, `formData.verify_content_timeout_seconds`, `handleInputChange` (both files already use this pattern for sibling boolean/number fields).

- [ ] **Step 1: Add the import checkbox**

In `WorkersConfigSection.tsx`, immediately after the `filter_sample_files` `<label>` block (~line 245), add:

```tsx
							<label className="flex min-w-0 cursor-pointer items-start gap-3 rounded-xl border border-base-300/60 bg-base-100/40 p-4">
								<input
									type="checkbox"
									className="toggle toggle-primary toggle-sm mt-0.5 shrink-0"
									checked={formData.verify_content ?? false}
									disabled={isReadOnly}
									onChange={(e) => handleInputChange("verify_content", e.target.checked)}
								/>
								<div className="min-w-0">
									<span className="block break-words font-bold text-xs">Verify Media Content</span>
									<span className="mt-0.5 block break-words text-[11px] text-base-content/50 leading-snug">
										After import, read each video/audio file's header through the serving
										stack and fail the import if no recognized media container signature is
										found. Catches releases assembled in the wrong order.
									</span>
								</div>
							</label>
```

- [ ] **Step 2: Add the health checkbox**

In `HealthConfigSection.tsx`, immediately after the `verify_data`/"Ghost File Detection" `<label>` block (~line 514, still inside the `!formData.check_all_segments &&` guard's sibling scope — place it outside that guard since content verification is independent of segment sampling mode), add:

```tsx
								<label className="flex cursor-pointer items-start gap-3 rounded-xl border border-base-300/60 bg-base-100/40 p-4">
									<input
										type="checkbox"
										className="checkbox checkbox-sm checkbox-primary mt-0.5 shrink-0"
										checked={formData.verify_content ?? false}
										disabled={isReadOnly}
										onChange={(e) => handleInputChange("verify_content", e.target.checked)}
									/>
									<div className="min-w-0 flex-1">
										<span className="block break-words font-bold text-xs">
											Verify Media Content
										</span>
										<span className="mt-0.5 block break-words text-[11px] text-base-content/50 leading-snug">
											On a file's first health check, read its header through the serving
											stack and mark it corrupted if no recognized media container signature
											is found.
										</span>
									</div>
								</label>
```

- [ ] **Step 3: Manually verify in the browser**

Run the dev server (`~/altmount-dev/dev-env.sh` / `run.sh` per project memory, or `cd frontend && bun run dev` against a running backend), navigate to the Import and Health config sections, and confirm both new checkboxes render, toggle, and persist across a save/reload.

- [ ] **Step 4: Run frontend checks**

Run: `cd frontend && bun run check`
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/config/WorkersConfigSection.tsx frontend/src/components/config/HealthConfigSection.tsx
git commit -m "feat(frontend): add verify_content toggles to import and health config"
```

---

### Task 10: Health error-type UI labels

**Files:**
- Modify: the frontend component(s) that render `HealthErrorDetails.error_type` for a file's health event (search first: `grep -rn "metadata_gap\|missing_segments" frontend/src` to find the exact display component).

**Interfaces:**
- Consumes: the `content_invalid` / `content_segment_missing` string values Task 6 writes into `HealthErrorDetails.ErrorType`.

- [ ] **Step 1: Locate the existing error-type label mapping**

Run: `grep -rn "metadata_gap\|missing_segments" frontend/src`

- [ ] **Step 2: Add the two new labels**

Add `content_invalid` → e.g. "Invalid media content" and `content_segment_missing` → e.g. "Content header unavailable" to whatever mapping (object/switch) that search turns up, following its existing structure exactly (do not invent a new mapping mechanism if one already exists).

- [ ] **Step 3: Manually verify in the browser**

With `health.verify_content` enabled and a deliberately-mislabeled test file (or by temporarily forcing `ContentInvalid` in a local build), confirm the health event list renders the new label instead of a raw `content_invalid` string.

- [ ] **Step 4: Run frontend checks**

Run: `cd frontend && bun run check`

- [ ] **Step 5: Commit**

```bash
git add <files touched in Step 2>
git commit -m "feat(frontend): add labels for content verification health error types"
```

---

### Task 11: End-to-end AES-encrypted RAR content probe test

**Files:**
- Test: `internal/contentverify/probe_integration_test.go` (or alongside existing RAR/AES test fixtures if `internal/importer/filesystem` or `internal/importer/archive/rar` already has reusable ones — check first with `grep -rln "DecryptingFileSystem\|aescipher" --include=*_test.go internal/importer`)

**Interfaces:**
- Consumes: `contentverify.Probe`, the real (or minimally faked) AES-decrypting read path.

- [ ] **Step 1: Find existing AES/RAR test fixtures**

Run: `grep -rln "DecryptingFileSystem\|aescipher\|Encryption_AES" --include=*_test.go internal/importer internal/nzbfilesystem`

Read whatever fixture-building helpers this turns up (a test NZB + AES key/IV + a fake NNTP body source) — reuse them rather than building new ones from scratch.

- [ ] **Step 2: Write the failing test**

```go
// internal/contentverify/probe_integration_test.go
package contentverify_test

import (
	"context"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/contentverify"
	// import whatever fixture-building package Step 1 identified
)

// TestProbe_AESEncryptedRARContent confirms Probe needs no special-casing
// for AES-encrypted RAR-backed virtual files: MetadataVirtualFile already
// decrypts before Read returns bytes, so a valid inner media signature
// must be detected exactly as for an unencrypted file.
func TestProbe_AESEncryptedRARContent(t *testing.T) {
	opener := buildAESEncryptedFixture(t) // from Step 1's helpers: an Opener
	// backed by real MetadataVirtualFile decryption logic, whose plaintext
	// starts with a valid MKV/EBML signature.

	res := contentverify.Probe(context.Background(), opener, "/movie.mkv", time.Second)

	if res.Result != contentverify.ContentValid {
		t.Errorf("got %v, want ContentValid for AES-decrypted content with a valid signature", res.Result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/contentverify/... -run TestProbe_AESEncryptedRARContent -v`
Expected: FAIL until the fixture wiring is complete (or compiles-but-fails if the fixture accidentally returns non-media bytes — adjust fixture data to a real MKV/EBML header prefix).

- [ ] **Step 3: Make it pass**

Wire `buildAESEncryptedFixture` using the fixtures found in Step 1. No production code changes are expected here — this task only proves Task 3's design assumption (no RAR/AES special-casing needed) holds against real decryption code, not a fake.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/contentverify/... -run TestProbe_AESEncryptedRARContent -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/contentverify/probe_integration_test.go
git commit -m "test(contentverify): verify AES-encrypted RAR content probes without special-casing"
```

---

### Task 12: Remove the design spec

**Files:**
- Delete: `docs/superpowers/specs/2026-08-27-media-content-verification-design.md`

- [ ] **Step 1: Remove the spec file now that the feature is implemented**

```bash
git rm docs/superpowers/specs/2026-08-27-media-content-verification-design.md
git commit -m "docs: remove media content verification spec (implemented)"
```
