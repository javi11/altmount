# Memory Tier for the Segment Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve recently decoded articles from memory across handles and re-opens (spec phase 6, defect D6), and take the disk cache write off the download goroutine.

**Architecture:** `segcache.MemoryCache` is a sharded, byte-bounded CLOCK (second-chance) cache of decoded articles keyed by message-ID. `segcache.TieredStore` implements `usenet.SegmentStore`: `Get` tries memory, then disk (promoting hits); `Put` stores in memory and queues the disk write on a bounded async writer. `segcache.Source.Store()` returns the tiered store whenever either tier is enabled, so the memory tier is on by default (256 MB) even with the disk cache off. The per-handle `randomReadCache` stays (cheap, and still useful when the memory tier is disabled).

**Spec:** `docs/superpowers/specs/2026-09-04-streaming-demand-shaping-design.md` (Phase 6). Not done from the spec: deriving `debug.SetMemoryLimit`; the Go collector handles a 256 MB cache fine and a wrong limit would hurt more than help.

## Global Constraints

- No competitor names in code, comments, commits, PRs.
- Conventional Commits; branch `feat/streaming-memory-segcache` from `feat/streaming-flight-dedup`.
- `make bench-compare BASE=baseline-main`: no regressions; new B10 replay scenario must show near-zero refetch.

---

### Task 1: B10 replay scenario, pre-sampled

`BenchmarkStreamReplay` (premium): read the first 32 MB, close, reopen, read the first 32 MB again. Metrics: `replay_bytes_fetched_mb` (gated, lower better), `replay_read_ms` (gated, tolerance 0.10). The harness gains `h.store usenet.SegmentStore` used by `openFile`; B10 sets it (nil on this branch's pre-sample).

- [ ] Add, commit `test(bench): reopen replay scenario`, sample once.

### Task 2: `MemoryCache`

**Files:** `internal/nzbfilesystem/segcache/memory.go`, `memory_test.go`.

```go
type MemoryCache struct{ /* 16 shards of map[string]*memEntry; one clock list under orderMu; size, capacity */ }
func NewMemoryCache(maxBytes int64) *MemoryCache
func (m *MemoryCache) Get(id string) ([]byte, bool) // read lock on the shard, sets touched
func (m *MemoryCache) Put(id string, data []byte)   // charges cap(data); evicts with second chance; skips articles larger than the budget
func (m *MemoryCache) SetCapacity(maxBytes int64)
func (m *MemoryCache) Size() int64
func (m *MemoryCache) Len() int
```
Lock order: `orderMu` then shard mutex, never reversed (two puts of one id must not double-charge).

- [ ] Tests: hit/miss; capacity bound (put 10 × 1 MB into 4 MB → Len ≤ 4, Size ≤ cap); second chance keeps a touched entry over an untouched older one; oversize article not cached; `Put` of an existing id replaces without leaking bytes; `SetCapacity` shrink evicts.
- [ ] Commit `feat(segcache): byte-bounded in-memory tier with second-chance eviction`.

### Task 3: `TieredStore` and `Source`

**Files:** `internal/nzbfilesystem/segcache/tiered.go`, `tiered_test.go`, `source.go`, `internal/config/manager.go` (`SegmentCacheConfig.MemoryMB *int`, default 256), `config.sample.yaml`.

```go
type TieredStore struct{ mem *MemoryCache; disk atomic.Pointer[diskRef]; writes chan diskWrite; dropped atomic.Int64 }
func NewTieredStore(mem *MemoryCache) *TieredStore // starts one writer goroutine
func (t *TieredStore) SetDisk(d usenet.SegmentStore) // nil disables the disk tier
func (t *TieredStore) Get(id string) ([]byte, bool)
func (t *TieredStore) Put(id string, data []byte) error // memory now, disk queued (queue 64, drop on overflow, count)
func (t *TieredStore) Dropped() int64
```
`Source.Store()`: `mem` enabled when `MemoryMB > 0` (capacity refreshed from config each call), `disk` when the existing conditions hold; nil when neither. Returns the one long-lived `TieredStore`.

- [ ] Tests: memory hit avoids disk; disk hit is promoted to memory; `Put` reaches the disk store asynchronously (poll); overflow drops and counts; `Source.Store()` with disk disabled and memory enabled returns a store; both disabled returns nil.
- [ ] Commit `feat(segcache): tiered store with async disk writes; memory tier on by default`.

### Task 4: Benchmark, live, PR

- [ ] B10 in the bench uses `segcache.NewTieredStore(segcache.NewMemoryCache(256 << 20))`; `make bench-stream`; B10 refetch ≈ 0 MB, replay read time ≪ first read; other scenarios unchanged.
- [ ] Live: `bench-live.sh memory-tier` plus a repeat of the two-reader check.
- [ ] Push; `gh pr create --base feat/streaming-flight-dedup`.
