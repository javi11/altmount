# PAR2 Repair Continuous Sweep Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the PAR2 repair sweep faster by keeping the article prefetch pipeline full across recovery-set file boundaries and removing a redundant full-release memory pass.

**Architecture:** `sweep` in `internal/par2repair/job.go` currently opens one `prefetchArticles` pipeline per plan file and drains it at each file end, so every file boundary idles the repair connections for roughly one round trip × depth. The plan flattens every live article of the recovery set into one sequence and runs a single pipeline over it. Separately, `completeSlice` memsets the whole slice buffer after every slice even though the next slice overwrites it fully; only the final partial slice of a file needs zero padding.

**Tech Stack:** Go 1.27, existing `par2repair` test fixtures (`mkRepairFixture`, `concurrencyFetcher`, `blockingFetcher`).

**Spec:** The comparison in this session against the reference implementation: its `feedPresent` resolves the whole plan before reading so "the prefetch can run on across file boundaries rather than draining at each one".

## Global Constraints

- Conventional Commits on every commit; branch prefixed `perf/`.
- No competitor names in code, comments, or commit messages.
- Run `go test ./internal/par2repair/` green before each commit; `go vet` clean.
- Default comments policy: comment only non-obvious WHY.

---

### Task 1: One prefetch pipeline across all recovery-set files

**Files:**
- Modify: `internal/par2repair/job.go` (`sweep`, ~lines 704-832)
- Test: `internal/par2repair/job_test.go`

**Interfaces:**
- Consumes: `prefetchArticles(ctx, fetch, arts []Article, depth func() int) (<-chan chan fetchResult, stop func())` — already skips `Dead` articles and yields one result channel per live article in order.
- Produces: no signature change to `sweep` or `RunJob`.

- [ ] **Step 1: Write the failing test** — a fetcher that records, for each fetch, which other message IDs are in flight at that moment. With one pipeline over the whole release, some fetch of a `b.rar` article must overlap a fetch of an `a.rar` article. With the per-file pipeline that overlap is impossible.

```go
// overlapFetcher records whether fetches from two different files were ever
// in flight together.
type overlapFetcher struct {
	inner *fakeFetcher

	mu       sync.Mutex
	inFlight map[string]int // file prefix -> count
	crossed  bool
}

func filePrefix(messageID string) string {
	return messageID[:strings.IndexByte(messageID, '-')]
}

func (o *overlapFetcher) Fetch(ctx context.Context, messageID string) ([]byte, error) {
	p := filePrefix(messageID)
	o.mu.Lock()
	o.inFlight[p]++
	for other, n := range o.inFlight {
		if other != p && n > 0 {
			o.crossed = true
		}
	}
	o.mu.Unlock()
	time.Sleep(2 * time.Millisecond)
	defer func() {
		o.mu.Lock()
		o.inFlight[p]--
		o.mu.Unlock()
	}()
	return o.inner.Fetch(ctx, messageID)
}

// The sweep's prefetch must not drain at recovery-set file boundaries: the
// first articles of the next file must already be in flight while the last
// articles of the previous one are still downloading.
func TestRunJobPrefetchSpansFileBoundaries(t *testing.T) {
	fx := mkRepairFixture(t, 1024, 512, 6, 1) // 16 articles per file
	fetch := &overlapFetcher{inner: fx.fetch, inFlight: map[string]int{}}

	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	store := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), plan, fx.idx, fx.par2Files, fetch, store, testLogger(), WithConcurrency(8)); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Get(fx.deadMsgID)
	if !ok || !bytes.Equal(got, fx.deadOrig) {
		t.Fatal("continuous sweep must still produce a byte-exact patch")
	}
	if !fetch.crossed {
		t.Fatal("no fetch of the second file overlapped a fetch of the first: the pipeline drained at the file boundary")
	}
}
```

Note: PAR2 file articles are named `<par2-N@test>` and are fetched before the sweep by `loadRecoveryPayloads`; their prefix `<par2` differs from `<a.rar` / `<b.rar`, and they are consumed before the sweep starts, so they cannot produce a false positive.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/par2repair/ -run TestRunJobPrefetchSpansFileBoundaries -count=3 -v`
Expected: FAIL with "the pipeline drained at the file boundary".

- [ ] **Step 3: Implement** — in `sweep`, build the flat live-article list before the file loop, open one pipeline, and pull from it inside the loop. Remove the per-file `prefetchArticles`/`stop()` pairs; one deferred `stop()`.

```go
	// One pipeline over every article of the recovery set, so the prefetch
	// stays full across file boundaries instead of draining at each one.
	var all []Article
	for _, f := range plan.Files {
		all = append(all, f.Articles...)
	}
	slots, stop := prefetchArticles(ctx, fetch, all, depth)
	defer stop()

	for fi, f := range plan.Files {
		...
		for ai, a := range f.Articles {
			if err := ctx.Err(); err != nil {
				return err
			}
			...
			slot, ok := <-slots
			if !ok {
				return ctx.Err()
			}
			...
		}
		if fill > 0 { ... }
	}
```

Every `stop(); return ...` inside the loop becomes a plain `return ...`.

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/par2repair/ -count=1`
Expected: PASS, including `TestRunJobAbsorbsMidSweepDeadArticle`, `TestRunJobMidSweepDeadWithoutMarginSurfacesError`, `TestRunJobHonorsConcurrencyOption`.

- [ ] **Step 5: Commit**

```bash
git add internal/par2repair/job.go internal/par2repair/job_test.go
git commit -m "perf(par2repair): keep the sweep prefetch full across file boundaries"
```

### Task 2: Zero-pad only the final partial slice

**Files:**
- Modify: `internal/par2repair/job.go` (`completeSlice`, final-partial handling in `sweep`)
- Test: `internal/par2repair/job_test.go`

- [ ] **Step 1: Write the guarding test** — a fixture whose file length is not a multiple of the slice size, so the last slice of each file is partial and depends on zero padding. `mkRepairFixture` builds 8192-byte files; slice size 1020 (a multiple of 4) leaves a 32-byte tail.

```go
// Files whose length is not a multiple of the slice size end in a partial
// slice that PAR2 defines as zero-padded; the sweep must fold it padded, and
// the repair must still be byte-exact.
func TestRunJobRepairsWithPartialFinalSlice(t *testing.T) {
	fx := mkRepairFixture(t, 1020, 2048, 6, 1) // 8192 % 1020 = 32-byte tail
	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	store := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), plan, fx.idx, fx.par2Files, fx.fetch, store, testLogger()); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Get(fx.deadMsgID)
	if !ok || !bytes.Equal(got, fx.deadOrig) {
		t.Fatal("partial-final-slice release must produce a byte-exact patch")
	}
}
```

- [ ] **Step 2: Run it** — it passes on current code (this is a refactor guard). Then temporarily delete `clear(buf)` in `completeSlice`, run again, and confirm it FAILS (CRC mismatch or checksum failure). Restore before Step 3.

Run: `go test ./internal/par2repair/ -run TestRunJobRepairsWithPartialFinalSlice -v`

- [ ] **Step 3: Implement** — drop the per-slice `clear(buf)`; a full slice overwrites every byte of `buf`. Zero only the tail before completing a partial final slice:

```go
		if fill > 0 {
			clear(buf[fill:])
			if err := completeSlice(); err != nil {
				return err
			}
		}
```

and remove `clear(buf)` from `completeSlice`.

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/par2repair/ -count=1 && go vet ./internal/par2repair/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/par2repair/job.go internal/par2repair/job_test.go
git commit -m "perf(par2repair): zero-pad only the final partial slice in the sweep"
```

### Task 3: Full build gate

- [ ] **Step 1:** Run `go build ./... && go test ./internal/par2repair/... ./internal/importer/parser/par2/...`
- [ ] **Step 2:** Push branch `perf/par2repair-continuous-sweep` and open a PR titled `perf(par2repair): continuous cross-file sweep prefetch`.
