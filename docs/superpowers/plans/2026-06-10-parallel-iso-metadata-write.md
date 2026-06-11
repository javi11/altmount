# Parallel Bare-ISO Metadata Write Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Parallelize the metadata write loop in `expandBareISOFiles` so multi-disc Blu-ray releases (2–4 ISOs) write their metadata concurrently instead of sequentially.

**Architecture:** The existing sequential `for` loop in `expandBareISOFiles` calls `deps.writeMetadata` for each expanded ISO one at a time. Replace that loop body with a `concpool` worker pool (the same pattern already used by `multifile/processor.go` and `rar/aggregator.go`). Untransformed ISOs (pass-through path) stay sequential — they hit `continue` before any goroutine is spawned, so no mutex is needed for the `remaining` slice. Only the `written` slice is appended from goroutines and needs a `sync.Mutex`.

**Tech Stack:** Go 1.26, `github.com/sourcegraph/conc/pool` (`concpool`), `sync.Mutex`, standard `go test -race`

---

## File Map

| File | Change |
|------|--------|
| `internal/importer/iso_expand.go` | Replace sequential write loop with `concpool`; add `"sync"` and `concpool` imports |
| `internal/importer/iso_expand_test.go` | Add test for multi-ISO parallel write; verify race detector is clean |

---

### Task 1: Parallelize the write loop in `expandBareISOFiles`

**Files:**
- Modify: `internal/importer/iso_expand.go`

- [ ] **Step 1: Read the current file to confirm baseline**

```bash
cat -n internal/importer/iso_expand.go
```
Expected: sequential `for i, c := range expanded` loop at ~line 100, no concpool import.

- [ ] **Step 2: Replace the imports block**

Current imports in `iso_expand.go`:
```go
import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)
```

Replace with:
```go
import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"
	"sync"

	concpool "github.com/sourcegraph/conc/pool"

	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)
```

- [ ] **Step 3: Replace the sequential loop body in `expandBareISOFiles`**

Locate and replace the sequential loop (starting at `for i, c := range expanded {`):

**Old code (lines ~100–123):**
```go
	for i, c := range expanded {
		if c.ISOExpansionIndex == 0 && len(c.NestedSources) == 0 {
			// Untransformed — fall back to standard processing.
			// len(expanded) <= len(isos) is guaranteed by archive.ExpandISOContents:
			// it appends one Content per input ISO on passthrough and ≤ one per
			// group on success. Index isos[i] is therefore safe here.
			remaining = append(remaining, isos[i])
			continue
		}
		meta := archive.NewFileMetadataFromContent(c, sourceNzbPath, releaseDate, c.NzbdavID)
		virtualPath := path.Join(virtualDir, c.Filename)
		if err := deps.writeMetadata(virtualPath, meta); err != nil {
			return written, nil, fmt.Errorf("write metadata %q: %w", virtualPath, err)
		}
		written = append(written, virtualPath)
		slog.InfoContext(ctx, "Expanded bare ISO into virtual file",
			"release", releaseName,
			"path", virtualPath,
			"size", c.Size,
			"nested_sources", len(c.NestedSources),
		)
	}
	remaining = append(remaining, rest...)
	return written, remaining, nil
```

**New code:**
```go
	var writtenMu sync.Mutex
	pl := concpool.New().WithErrors().WithFirstError().WithContext(ctx)

	for i, c := range expanded {
		if c.ISOExpansionIndex == 0 && len(c.NestedSources) == 0 {
			// Untransformed — fall back to standard processing.
			// len(expanded) <= len(isos) is guaranteed by archive.ExpandISOContents:
			// it appends one Content per input ISO on passthrough and ≤ one per
			// group on success. Index isos[i] is therefore safe here.
			// Collected in this goroutine before pl.Go is called, so no mutex needed.
			remaining = append(remaining, isos[i])
			continue
		}
		pl.Go(func(ctx context.Context) error {
			meta := archive.NewFileMetadataFromContent(c, sourceNzbPath, releaseDate, c.NzbdavID)
			virtualPath := path.Join(virtualDir, c.Filename)
			if err := deps.writeMetadata(virtualPath, meta); err != nil {
				return fmt.Errorf("write metadata %q: %w", virtualPath, err)
			}
			writtenMu.Lock()
			written = append(written, virtualPath)
			writtenMu.Unlock()
			slog.InfoContext(ctx, "Expanded bare ISO into virtual file",
				"release", releaseName,
				"path", virtualPath,
				"size", c.Size,
				"nested_sources", len(c.NestedSources),
			)
			return nil
		})
	}

	if err := pl.Wait(); err != nil {
		return written, nil, err
	}

	remaining = append(remaining, rest...)
	return written, remaining, nil
```

- [ ] **Step 4: Build to verify no compile errors**

```bash
cd internal/importer && go build ./...
```
Expected: exits 0 with no output.

- [ ] **Step 5: Commit**

```bash
git add internal/importer/iso_expand.go
git commit -m "perf(importer): parallelize bare-ISO metadata writes using concpool"
```

---

### Task 2: Add parallel-write test and run race detector

**Files:**
- Modify: `internal/importer/iso_expand_test.go`

- [ ] **Step 1: Add import for `sync` and `strings` to the test file**

Current test imports:
```go
import (
	"context"
	"testing"

	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)
```

Replace with:
```go
import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)
```

- [ ] **Step 2: Append the new test at end of file**

Add after the last test function:

```go
// TestExpandBareISOFiles_MultipleISOs_WritesAllInParallel verifies that when
// multiple ISOs expand successfully, all their metadata is written and the
// race detector finds no data races. The writeMetadata mock is intentionally
// not protected by a mutex so the race detector catches any missing
// synchronisation in the implementation.
func TestExpandBareISOFiles_MultipleISOs_WritesAllInParallel(t *testing.T) {
	files := []parser.ParsedFile{
		{Filename: "DISC_1.iso", Size: 1000},
		{Filename: "DISC_2.iso", Size: 2000},
		{Filename: "DISC_3.iso", Size: 3000},
	}

	var writtenMu sync.Mutex
	var writtenPaths []string

	deps := expandBareISODeps{
		enabled: true,
		expand: func(_ context.Context, _ bool, in []archive.Content) ([]archive.Content, error) {
			out := make([]archive.Content, len(in))
			for i, c := range in {
				out[i] = archive.Content{
					Filename: strings.TrimSuffix(c.Filename, ".iso") + ".m2ts",
					Size:     c.Size,
					NestedSources: []archive.NestedSource{
						{InnerOffset: 0, InnerLength: c.Size},
					},
				}
			}
			return out, nil
		},
		writeMetadata: func(virtualPath string, _ *metapb.FileMetadata) error {
			writtenMu.Lock()
			writtenPaths = append(writtenPaths, virtualPath)
			writtenMu.Unlock()
			return nil
		},
	}

	written, rest, err := expandBareISOFiles(context.Background(), deps, files, "vdir", "movie", "", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(written) != 3 {
		t.Errorf("written = %v, want 3 paths", written)
	}
	if len(rest) != 0 {
		t.Errorf("rest = %v, want empty (all ISOs expanded)", rest)
	}

	sort.Strings(writtenPaths)
	want := []string{"vdir/DISC_1.m2ts", "vdir/DISC_2.m2ts", "vdir/DISC_3.m2ts"}
	for i, w := range want {
		if i >= len(writtenPaths) || writtenPaths[i] != w {
			t.Errorf("writtenPaths[%d] = %q, want %q", i, writtenPaths[i], w)
		}
	}
}
```

- [ ] **Step 3: Run existing tests to confirm no regressions**

```bash
go test -v ./internal/importer/... -run TestExpandBareISO
```
Expected: all 5 tests PASS.

- [ ] **Step 4: Run with race detector**

```bash
go test -race ./internal/importer/... -run TestExpandBareISO
```
Expected: exits 0, `PASS`, no `DATA RACE` output.

- [ ] **Step 5: Run full importer test suite with race detector**

```bash
go test -race ./internal/importer/...
```
Expected: exits 0, `PASS`.

- [ ] **Step 6: Commit**

```bash
git add internal/importer/iso_expand_test.go
git commit -m "test(importer): add parallel multi-ISO metadata write test with race detection"
```

---

## Verification

End-to-end check (no real NZB needed — unit tests cover the logic):

```bash
go test -race ./internal/importer/...
```

Expected output (all green, no races):
```
ok  	github.com/javi11/altmount/internal/importer	0.XXXs
ok  	github.com/javi11/altmount/internal/importer/multifile	0.XXXs
...
```

To confirm the full build is still green:
```bash
make
```
