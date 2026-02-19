# Segment-Intelligent Cache: Implementation Plan

## Problem Statement

The current VFS cache (`internal/fuse/vfs/`) operates at **8MB arbitrary chunk boundaries**, but the
underlying download unit is a **Usenet segment** (~750KB each, variable). This misalignment means:

- A cache miss fetches an 8MB chunk even if 1KB was requested (blocks until ~11 segments download)
- Cache keys are byte offsets — no way to deduplicate the same segment requested via different
  file handles or paths
- Range tracking (coalesced intervals in `ranges.go`) is complex bookkeeping for something that
  is naturally boolean: *"is segment X cached or not?"*

The underlying `MetadataVirtualFile.ReadAt` in `nzbfilesystem` already uses a `segmentOffsetIndex`
for O(1) offset→segment mapping. The NNTP download is already segment-aligned. The VFS cache is
an extra layer with the wrong granularity.

## Architecture of the New Cache

```
FUSE Handle.Read(off, size)
    → SegmentCachedFile.ReadAt(off, size)
        → segmentIndex: find segments covering [off, off+size)
        → For each segment i:
            ├── cache.Has(seg.MessageID) → true: read from disk file
            └── false: fetchAndCache(seg) via singleflight.Do(messageID, ...)
                    → opener.Open(ctx, path)  [= nzbfs.Open → MetadataVirtualFile]
                    → file.ReadAt(seg.FileStart, seg.UsableLen)  ← segment-aligned!
                    → cache.Put(seg.MessageID, data)
        → Assemble p[] from per-segment data
        → prefetcher.RecordAccess(segmentIndex)
```

Cache key: **Usenet Message ID** (globally unique, permanent).
Cache value: **decoded segment bytes** (output of yEnc decode + encryption handling).
Cache unit size: ~750KB per entry (matches actual NNTP download unit exactly).

## New Package: `internal/nzbfilesystem/segcache/`

Placed inside `nzbfilesystem` because it needs `metapb.SegmentData` types and integrates
at that layer.

---

## Step 1 — `segcache/cache.go`: Disk segment store

**What it does**: stores decoded segment bytes on disk, keyed by message ID.

```
CachePath/
├── <hex(sha256(messageID))>.seg    # raw decoded bytes for one segment
└── catalog.json                    # {messageID: {dataPath, size, lastAccess, created}}
```

**Types and functions to implement**:

```go
type Config struct {
    CachePath      string
    MaxSizeBytes   int64
    ExpiryDuration time.Duration
}

type SegmentCache struct { /* mu, items map[string]*entry, config, logger */ }

func NewSegmentCache(cfg Config, logger *slog.Logger) (*SegmentCache, error)
// - MkdirAll(CachePath)
// - Load catalog.json on startup (cross-check .seg files exist)

func (c *SegmentCache) Has(messageID string) bool                // O(1), no disk I/O
func (c *SegmentCache) Get(messageID string) ([]byte, bool)      // reads .seg file
func (c *SegmentCache) Put(messageID string, data []byte) error  // writes .seg file, updates catalog
func (c *SegmentCache) Evict()                                   // LRU until ≤ MaxSizeBytes
func (c *SegmentCache) Cleanup()                                 // remove expired entries
func (c *SegmentCache) TotalSize() int64
func (c *SegmentCache) ItemCount() int
func (c *SegmentCache) SaveCatalog() error                       // flush catalog.json
```

Notes:
- `Put` writes atomically (temp file + rename)
- `Get` updates `lastAccess` in memory (catalog flushed periodically by manager)
- Eviction: sort by `lastAccess` ASC, delete oldest until under `MaxSizeBytes`
- Unlike VFS cache: no sparse files, no range tracking, no per-file metadata files

---

## Step 2 — `segcache/file.go`: SegmentCachedFile

**Types**:

```go
// SegmentEntry is a precomputed view of one segment in file coordinates.
// Built once from fileMeta.SegmentData by the manager at Open() time.
type SegmentEntry struct {
    MessageID string
    FileStart int64    // cumulative start in file coordinates (inclusive)
    FileEnd   int64    // exclusive end in file coordinates
    Groups    []string // for error reporting only
}

// FileOpener matches vfs.FileOpener (reuse same interface shape).
type FileOpener interface {
    Open(ctx context.Context, name string) (afero.File, error)
}

type SegmentCachedFile struct {
    path       string
    fileSize   int64
    segments   []SegmentEntry       // sorted by FileStart
    cache      *SegmentCache
    opener     FileOpener
    prefetcher *Prefetcher
    fetchGroup singleflight.Group   // keyed by MessageID
    closed     atomic.Bool
    logger     *slog.Logger
}
```

**`ReadAt(p []byte, off int64) (int, error)`**:

```
1. Bounds check (off >= fileSize → io.EOF)
2. Clamp end = min(off+len(p), fileSize)
3. Binary search segments: first i where segments[i].FileEnd > off
4. For each segment i covering [off, end):
   a. singleflight.Do(seg.MessageID):
        if !cache.Has(seg.MessageID) { fetchAndCache(ctx, seg) }
   b. data, _ := cache.Get(seg.MessageID)
   c. segReadStart := off - seg.FileStart        (offset within segment data)
   d. copy p[pOff:] from data[segReadStart:]
   e. advance pOff, off
5. prefetcher.RecordAccess(segmentIndex of first accessed segment)
```

**`fetchAndCache(ctx, seg SegmentEntry) error`**:

```
1. ctx with 60s timeout
2. file, err := opener.Open(ctx, path)   [= MetadataVirtualFile]
3. buf := make([]byte, seg.FileEnd - seg.FileStart)
4. n, err := file.ReadAt(buf, seg.FileStart)
   // MetadataVirtualFile.ReadAt internally does:
   //   findSegmentForOffset(seg.FileStart) → segment i
   //   createUsenetReader(ctx, seg.FileStart, seg.FileEnd-1)
   //   → downloads exactly segment i from NNTP
5. cache.Put(seg.MessageID, buf[:n])
```

The key insight: since we align the `ReadAt` call to exact segment boundaries, the
`MetadataVirtualFile` downloads **exactly one segment** from NNTP, nothing more.

---

## Step 3 — `segcache/prefetcher.go`: Sequential segment prefetch

Mirrors `vfs.Downloader` but operates on **segment indices** (integers), not byte offsets.

```go
type Prefetcher struct {
    segments            []SegmentEntry
    cache               *SegmentCache
    opener              FileOpener
    path                string
    readAheadCount      int
    prefetchConcurrency int

    fetchGroup *singleflight.Group  // shared with SegmentCachedFile

    // Sequential detection
    mu             sync.Mutex
    lastSegIdx     int
    sequentialHits int
    isSequential   bool

    // Circuit breaker
    consecutiveErrors atomic.Int32
    circuitOpen       atomic.Bool
    circuitOpenedAt   atomic.Int64

    // Lifecycle (same pattern as Downloader)
    prefetchCancel context.CancelFunc
    wg             sync.WaitGroup
    ctx            context.Context
    stopped        atomic.Bool
    lastSeen       atomic.Int64
}

func (p *Prefetcher) RecordAccess(segIdx int)
// - Sequential if delta == 1 (adjacent segment, not arbitrary bytes)
// - Simpler than Downloader: delta is always 1 for truly sequential playback
// - On seek (delta != 1): cancel running prefetch

func (p *Prefetcher) prefetchWithCtx(ctx context.Context, fromIdx int)
// - Fetch segments [fromIdx+1 .. fromIdx+readAheadCount]
// - Skip if cache.Has(seg.MessageID)
// - errgroup.SetLimit(prefetchConcurrency)
// - Each fetch: singleflight.Do(messageID, fetchSegmentFromOpener)
```

Advantages vs `vfs.Downloader`:
- Sequential detection is exact: segment indices differ by exactly 1 for true sequential reads
- No chunk-alignment arithmetic: segment boundaries are natural
- Prefetch count is in segments (~750KB each) not 8MB chunks → finer control

---

## Step 4 — `segcache/manager.go`: Lifecycle manager

```go
type ManagerConfig struct {
    Enabled             bool
    CachePath           string
    MaxSizeBytes        int64
    ExpiryDuration      time.Duration
    ReadAheadSegments   int   // default 8 (≈6MB ahead, comparable to current 6×8MB=48MB)
    PrefetchConcurrency int   // default 3
}

type StatsSnapshot struct {
    CacheHits    int64
    CacheMisses  int64
    TotalSize    int64
    ItemCount    int
    ActiveFiles  int64
}

type Manager struct {
    cache       *SegmentCache
    config      ManagerConfig
    logger      *slog.Logger
    prefetchers sync.Map   // path → *Prefetcher
    activeFiles atomic.Int64
    hits        atomic.Int64
    misses      atomic.Int64
    ctx         context.Context
    cancel      context.CancelFunc
    wg          sync.WaitGroup
}

func NewManager(cfg ManagerConfig, logger *slog.Logger) (*Manager, error)
func (m *Manager) Start(ctx context.Context)
// - Background: cleanupLoop (5 min interval: cache.Cleanup() + cache.Evict())
// - Background: catalogFlushLoop (10 sec interval: cache.SaveCatalog())
// - Background: idleMonitor (30 sec: stop Prefetchers not seen for >30s)

func (m *Manager) Stop()
// - cancel() + wait goroutines + final SaveCatalog()

func (m *Manager) Open(
    path string,
    segments []SegmentEntry,
    fileSize int64,
    opener FileOpener,
) (*SegmentCachedFile, error)
// - Get/create Prefetcher for path (one per path, shared across concurrent handles)
// - Create SegmentCachedFile sharing the Prefetcher's fetchGroup

func (m *Manager) Close(path string)
// - m.activeFiles.Add(-1)

func (m *Manager) GetStats() StatsSnapshot
```

---

## Step 5 — `nzbfilesystem/nzbfilesystem.go`: Expose segment entries

The FUSE layer needs to know the segment layout for a file before opening the segment cache.
Add one new method to `NzbFilesystem`:

```go
// GetSegmentEntries returns the precomputed segment entries for a virtual file.
// Used by the segment cache layer to map file offsets to Usenet message IDs.
func (fs *NzbFilesystem) GetSegmentEntries(
    ctx context.Context,
    path string,
) (entries []segcache.SegmentEntry, fileSize int64, err error)
```

Implementation:
1. `fileMeta, err := fs.metadataService.ReadFileMetadata(ctx, path)`
2. Build `[]SegmentEntry` from `fileMeta.SegmentData`:
   - Iterate segments, accumulate cumulative `FileStart`/`FileEnd` from `seg.EndOffset - seg.StartOffset + 1`
   - `MessageID = seg.Id`, `Groups = seg.Groups`
3. Return entries and `fileMeta.FileSize`

This is essentially the same logic as `buildSegmentIndex` in `metadata_remote_file.go` but returns
the richer `SegmentEntry` slice.

---

## Step 6 — `internal/config/manager.go`: New config fields

Add to `FuseConfig`:

```go
// Segment cache (replaces VFS disk cache for better performance)
SegmentCacheEnabled      *bool  `yaml:"segment_cache_enabled" mapstructure:"segment_cache_enabled" json:"segment_cache_enabled"`
SegmentCachePath         string `yaml:"segment_cache_path" mapstructure:"segment_cache_path" json:"segment_cache_path"`
SegmentCacheMaxSizeGB    int    `yaml:"segment_cache_max_size_gb" mapstructure:"segment_cache_max_size_gb" json:"segment_cache_max_size_gb"`
SegmentCacheExpiryH      int    `yaml:"segment_cache_expiry_hours" mapstructure:"segment_cache_expiry_hours" json:"segment_cache_expiry_hours"`
SegmentCacheReadAhead    int    `yaml:"segment_cache_read_ahead" mapstructure:"segment_cache_read_ahead" json:"segment_cache_read_ahead"`
SegmentCachePrefetchConc int    `yaml:"segment_cache_prefetch_concurrency" mapstructure:"segment_cache_prefetch_concurrency" json:"segment_cache_prefetch_concurrency"`
```

Defaults (applied in `server.go`, same pattern as VFS cache defaults):
- `SegmentCachePath`: `/tmp/altmount-segment-cache`
- `SegmentCacheMaxSizeGB`: 10
- `SegmentCacheExpiryH`: 24
- `SegmentCacheReadAhead`: 8   (≈6MB of lookahead at 750KB/segment)
- `SegmentCachePrefetchConc`: 3

---

## Step 7 — `internal/fuse/server.go`: Initialize segment cache manager

Add `segcacheMgr *segcache.Manager` field to `Server`.

In `Mount()`, after VFS cache initialization block:

```go
var segcacheMgr *segcache.Manager
if s.config.SegmentCacheEnabled != nil && *s.config.SegmentCacheEnabled {
    // ... resolve defaults for path, maxSizeGB, expiryH, readAhead, prefetchConc
    cfg := segcache.ManagerConfig{
        Enabled:             true,
        CachePath:           segCachePath,
        MaxSizeBytes:        int64(maxSizeGB) * 1024 * 1024 * 1024,
        ExpiryDuration:      time.Duration(expiryH) * time.Hour,
        ReadAheadSegments:   readAhead,
        PrefetchConcurrency: prefetchConc,
    }
    segcacheMgr, err = segcache.NewManager(cfg, s.logger.With("component", "segcache"))
    if err != nil {
        s.logger.Warn("Failed to create segment cache, running without cache", "error", err)
    } else {
        segcacheMgr.Start(context.Background())
    }
}
s.segcacheMgr = segcacheMgr
```

Pass `segcacheMgr` into `NewDir(...)`. Thread it through to `File` struct.

Stop `segcacheMgr` after `s.server.Wait()` (same as `vfsm.Stop()`).

---

## Step 8 — `internal/fuse/file.go`: Use segment cache in `Open()`

Add `segcacheMgr *segcache.Manager` field to `File`.

In `Open()`, add a new branch **before** the VFS cache branch:

```go
// Segment cache mode (preferred over VFS cache when enabled)
if f.segcacheMgr != nil {
    entries, fileSize, err := f.nzbfs.GetSegmentEntries(ctx, f.path)
    if err != nil {
        // fall through to VFS or fallback
        f.logger.WarnContext(ctx, "GetSegmentEntries failed, falling back", "path", f.path, "error", err)
    } else {
        opener := &suppressStreamOpener{inner: &nzbfsFileOpener{nzbfs: f.nzbfs}}
        segFile, err := f.segcacheMgr.Open(f.path, entries, fileSize, opener)
        if err != nil {
            if stream != nil { f.streamTracker.Remove(stream.ID) }
            f.logger.ErrorContext(ctx, "Segment cache Open failed", "path", f.path, "error", err)
            return nil, 0, syscall.EIO
        }
        handle := &Handle{
            segCachedFile: segFile,
            logger:        f.logger,
            path:          f.path,
            segcacheMgr:   f.segcacheMgr,
            stream:        stream,
            streamTracker: f.streamTracker,
        }
        return handle, fuse.FOPEN_KEEP_CACHE, 0
    }
}
// ... existing VFS cache branch ...
// ... existing fallback branch ...
```

---

## Step 9 — `internal/fuse/handle.go`: Add SegmentCachedFile branch

```go
type Handle struct {
    cachedFile    *vfs.CachedFile                  // VFS cache mode
    segCachedFile *segcache.SegmentCachedFile      // Segment cache mode (new)
    file          afero.File                       // Fallback
    closed        atomic.Bool
    logger        *slog.Logger
    path          string
    vfsm          *vfs.Manager
    segcacheMgr   *segcache.Manager                // (new)
    stream        *nzbfilesystem.ActiveStream
    streamTracker StreamTracker
    mu            sync.Mutex
    position      int64
}
```

In `Read()`:

```go
if h.segCachedFile != nil {
    n, err := h.segCachedFile.ReadAt(dest, off)
    if n > 0 && h.stream != nil {
        h.streamTracker.UpdateProgress(h.stream.ID, int64(n))
        atomic.StoreInt64(&h.stream.CurrentOffset, off+int64(n))
    }
    if err != nil {
        if err == io.EOF { ... }
        if errors.Is(err, context.Canceled) { ... return syscall.EINTR }
        return nil, syscall.EIO
    }
    return fuse.ReadResultData(dest[:n]), 0
}
```

In `Release()`:

```go
if h.segCachedFile != nil {
    _ = h.segCachedFile.Close()
    if h.segcacheMgr != nil {
        h.segcacheMgr.Close(h.path)
    }
    return 0
}
```

---

## Step 10 — Tests

| File | Tests |
|------|-------|
| `segcache/cache_test.go` | Put/Get/Has round-trip; LRU eviction removes oldest; expiry cleanup; catalog survives restart |
| `segcache/file_test.go` | ReadAt single segment; ReadAt spanning two segments; ReadAt at boundary; concurrent ReadAt; EOF clamp |
| `segcache/prefetcher_test.go` | Sequential detection (delta=1 → isSequential); non-sequential seek cancels prefetch; circuit breaker after 10 errors |
| `segcache/manager_test.go` | Open returns working CachedFile; shared Prefetcher for same path; Stop flushes catalog |

Use mock `FileOpener` (records which segment ranges were fetched) to verify segment-alignment.

---

## Migration / Rollback Strategy

1. Both caches coexist. Priority: **SegmentCache > VFS cache > fallback**.
2. `DiskCacheEnabled` continues to work unchanged.
3. `SegmentCacheEnabled` is opt-in (default false until validated).
4. After validation in production, deprecate `DiskCacheEnabled` in docs.
5. VFS cache code (`internal/fuse/vfs/`) is not deleted until segment cache is proven stable.

---

## File Change Summary

| Action | File |
|--------|------|
| **Create** | `internal/nzbfilesystem/segcache/cache.go` |
| **Create** | `internal/nzbfilesystem/segcache/file.go` |
| **Create** | `internal/nzbfilesystem/segcache/prefetcher.go` |
| **Create** | `internal/nzbfilesystem/segcache/manager.go` |
| **Create** | `internal/nzbfilesystem/segcache/cache_test.go` |
| **Create** | `internal/nzbfilesystem/segcache/file_test.go` |
| **Create** | `internal/nzbfilesystem/segcache/prefetcher_test.go` |
| **Create** | `internal/nzbfilesystem/segcache/manager_test.go` |
| **Modify** | `internal/nzbfilesystem/nzbfilesystem.go` — add `GetSegmentEntries` |
| **Modify** | `internal/config/manager.go` — add 6 new `FuseConfig` fields |
| **Modify** | `internal/fuse/server.go` — init `segcacheMgr`, pass to `NewDir` |
| **Modify** | `internal/fuse/file.go` — add `segcacheMgr` field, new Open branch |
| **Modify** | `internal/fuse/handle.go` — add `segCachedFile` field, new Read/Release branch |
