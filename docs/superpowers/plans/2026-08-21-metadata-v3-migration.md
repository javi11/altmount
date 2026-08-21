# Legacy Metadata → v3 Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate legacy inline-segment `.meta` files to the v3 store-backed format, merging each release onto one shared `.nzbz`, driven manually from the AltMount panel with a dry run first.

**Architecture:** A `MigrationWorker` in `internal/metadata` (modeled on `internal/health.LibrarySyncWorker`) walks the metadata root, groups legacy metas by release, builds one `NzbStore` per group (faithful from a surviving source `.nzb`, else synthesized from the inline segments), writes and verifies the `.nzbz`, then repoints each meta through the existing `MetadataService.WriteFileMetadataV3`. Fiber handlers expose status/dry-run/start/cancel; a React card in the metadata config section drives it.

**Tech Stack:** Go 1.x, protobuf (`google.golang.org/protobuf`), `github.com/javi11/nzbparser`, zstd (`klauspost/compress`), Fiber v2, React + TypeScript, TanStack Query, DaisyUI, Vitest-free (Go tests only), Bun.

**Spec:** `docs/superpowers/specs/2026-08-21-metadata-v1-to-v3-migration-design.md`

## Global Constraints

- Go logging MUST use `slog` context methods (`InfoContext`, `ErrorContext`, `WarnContext`, `DebugContext`), never bare `slog.Info`.
- API handlers MUST use the response builders in `internal/api/response.go` (`RespondSuccess`, `RespondConflict`, `RespondInternalError`, `RespondMessage`), never inline `c.JSON` envelopes.
- Frontend MUST use named exports, explicit `type` attributes on every `<button>`, DaisyUI components, Lucide icons, and direct imports (no barrel/`index.ts` files).
- Never delete or rewrite `.id` sidecar files; `ReadFileMetadata` populates `NzbdavId` from them and `WriteFileMetadata` leaves them alone.
- Store files are written under `<configDir>/.nzbs/_migrated/`, where `configDir` is the absolute `filepath.Dir(cfg.Database.Path)`.
- The default synthesized newsgroup is `alt.binaries.misc`.
- Conventional Commits on every commit (`feat:`, `fix:`, `refactor:`, `test:`, `docs:`, with optional scope).
- Never overwrite an existing `.nzbz` belonging to a different segment set: store filenames embed a content hash.

---

### Task 1: Move `BuildStore` into `internal/metadata`

The faithful-store path needs `BuildStore`, but `internal/metadata` must not depend on `internal/importer`. `internal/importer/parser` depends only on `internal/metadata/proto`, so moving the function to `internal/metadata` (where `NzbStore` lives) is cycle-free and keeps store construction in one place. There are exactly two call sites.

**Files:**
- Create: `internal/metadata/build_store.go`
- Create: `internal/metadata/build_store_test.go`
- Modify: `internal/importer/parser/parser.go:138` and delete `BuildStore` at `internal/importer/parser/parser.go:1478-1507`
- Delete: `internal/importer/parser/build_store_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `metadata.BuildStore(n *nzbparser.Nzb) (*metapb.NzbStore, map[string]int64)` — used by Task 5.

- [ ] **Step 1: Write the failing test**

Create `internal/metadata/build_store_test.go`:

```go
package metadata

import (
	"sort"
	"testing"

	"github.com/javi11/nzbparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildStore(t *testing.T) {
	nzb := &nzbparser.Nzb{
		Files: nzbparser.NzbFiles{
			{Subject: "Movie.mkv (1/2)", Poster: "p@x", Date: 1000, Groups: []string{"a.b.test"},
				Segments: nzbparser.NzbSegments{{ID: "m1@x", Number: 1, Bytes: 700000}, {ID: "m2@x", Number: 2, Bytes: 500000}}},
			{Subject: "Movie.par2 (1/1)", Poster: "p@x", Date: 1000, Groups: []string{"a.b.test"},
				Segments: nzbparser.NzbSegments{{ID: "p1@x", Number: 1, Bytes: 4096}}},
		},
	}
	for i := range nzb.Files {
		sort.Sort(nzb.Files[i].Segments)
	}

	store, index := BuildStore(nzb)
	require.NotNil(t, store)
	require.Len(t, store.Files, 2)
	assert.Equal(t, "Movie.mkv (1/2)", store.Files[0].Subject)
	assert.Equal(t, "p@x", store.Files[0].Poster)
	assert.EqualValues(t, 1000, store.Files[0].Date)
	assert.Equal(t, []string{"a.b.test"}, store.Files[0].Groups)
	require.Len(t, store.Files[0].Segments, 2)
	assert.Equal(t, "m1@x", store.Files[0].Segments[0].Id)
	assert.EqualValues(t, 700000, store.Files[0].Segments[0].Bytes)

	assert.EqualValues(t, 0, index["m1@x"])
	assert.EqualValues(t, 1, index["m2@x"])
	assert.EqualValues(t, 2, index["p1@x"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/ -run TestBuildStore -v`
Expected: FAIL — `undefined: BuildStore`

- [ ] **Step 3: Create the new file**

Create `internal/metadata/build_store.go`:

```go
package metadata

import (
	"sort"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/nzbparser"
)

// BuildStore converts a parsed NZB into a NzbStore (for persistence) plus a
// message-id → flat-store-index lookup used to emit SegmentRefs.
// Segments are stored in their natural NzbSegments order (by Number after sort).
func BuildStore(n *nzbparser.Nzb) (*metapb.NzbStore, map[string]int64) {
	store := &metapb.NzbStore{Files: make([]*metapb.NzbFileEntry, 0, len(n.Files))}
	index := make(map[string]int64)
	var flat int64
	for _, f := range n.Files {
		fe := &metapb.NzbFileEntry{
			Subject: f.Subject,
			Poster:  f.Poster,
			Date:    int64(f.Date),
			Groups:  f.Groups,
		}
		segs := make(nzbparser.NzbSegments, len(f.Segments))
		copy(segs, f.Segments)
		sort.Sort(segs)
		for _, s := range segs {
			fe.Segments = append(fe.Segments, &metapb.NzbSeg{
				Id:     s.ID,
				Number: int32(s.Number),
				Bytes:  int64(s.Bytes),
			})
			index[s.ID] = flat
			flat++
		}
		store.Files = append(store.Files, fe)
	}
	return store, index
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metadata/ -run TestBuildStore -v`
Expected: PASS

- [ ] **Step 5: Remove the old copy and repoint the caller**

Delete the whole `BuildStore` function from `internal/importer/parser/parser.go` (the block starting with the comment `// BuildStore converts a parsed NZB into a NzbStore (for persistence) plus a` and ending with the closing `}` of the function, around lines 1478-1507).

Delete `internal/importer/parser/build_store_test.go`.

Change `internal/importer/parser/parser.go:138` from:

```go
	parsed.Store, parsed.SegmentIndex = BuildStore(n)
```

to:

```go
	parsed.Store, parsed.SegmentIndex = metadata.BuildStore(n)
```

Add `"github.com/javi11/altmount/internal/metadata"` to the import block of `internal/importer/parser/parser.go`. If `sort` becomes unused there, leave it — it is used elsewhere in that file (`sort.Sort(segs)` appears in other functions); run the build to confirm.

- [ ] **Step 6: Verify the whole build and the parser package**

Run: `go build ./... && go test ./internal/importer/parser/ ./internal/metadata/ 2>&1 | tail -20`
Expected: build succeeds, both packages PASS (or `no test files` for a package without tests)

- [ ] **Step 7: Commit**

```bash
git add internal/metadata/build_store.go internal/metadata/build_store_test.go internal/importer/parser/parser.go
git rm internal/importer/parser/build_store_test.go
git commit -m "refactor(metadata): move BuildStore next to the NzbStore it builds"
```

---

### Task 2: Add `metadata.migration.default_group` config

**Files:**
- Modify: `internal/config/manager.go:326-330` (`MetadataConfig`) and the defaults block at `internal/config/manager.go:1686-1691`
- Modify: `frontend/src/types/config.ts:70-81`

**Interfaces:**
- Consumes: nothing.
- Produces: `config.MetadataMigrationConfig{DefaultGroup string}`, reachable as `cfg.Metadata.Migration.DefaultGroup`; TS `MetadataMigrationConfig { default_group: string }` on `MetadataConfig.migration`.

- [ ] **Step 1: Write the failing test**

Create `internal/config/metadata_migration_test.go`:

```go
package config

import "testing"

func TestDefaultConfig_MetadataMigrationDefaultGroup(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Metadata.Migration.DefaultGroup != "alt.binaries.misc" {
		t.Fatalf("expected default group alt.binaries.misc, got %q", cfg.Metadata.Migration.DefaultGroup)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestDefaultConfig_MetadataMigrationDefaultGroup -v`
Expected: FAIL — `cfg.Metadata.Migration undefined`

- [ ] **Step 3: Add the config struct and default**

In `internal/config/manager.go`, change `MetadataConfig` (line 326) to add the field:

```go
type MetadataConfig struct {
	RootPath                 string                  `yaml:"root_path" mapstructure:"root_path" json:"root_path"`
	DeleteSourceNzbOnRemoval *bool                   `yaml:"delete_source_nzb_on_removal" mapstructure:"delete_source_nzb_on_removal" json:"delete_source_nzb_on_removal,omitempty"`
	Backup                   MetadataBackupConfig    `yaml:"backup" mapstructure:"backup" json:"backup"`
	Migration                MetadataMigrationConfig `yaml:"migration" mapstructure:"migration" json:"migration"`
}

// MetadataMigrationConfig configures the legacy-metadata → v3 migration.
type MetadataMigrationConfig struct {
	// DefaultGroup is the newsgroup written into synthesized NzbStore entries.
	// Legacy metas do not retain the original groups, and nzb.BuildNZB renders an
	// empty <groups> element without this, which most NZB clients reject.
	DefaultGroup string `yaml:"default_group" mapstructure:"default_group" json:"default_group"`
}
```

In the defaults block (after the `Backup: MetadataBackupConfig{...}` literal ending at line 1691), add:

```go
			Migration: MetadataMigrationConfig{
				DefaultGroup: "alt.binaries.misc",
			},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestDefaultConfig_MetadataMigrationDefaultGroup -v`
Expected: PASS

- [ ] **Step 5: Add the TypeScript type**

In `frontend/src/types/config.ts`, change `MetadataConfig` (line 70) and add the new interface after `MetadataBackupConfig`:

```ts
export interface MetadataConfig {
	root_path: string;
	delete_source_nzb_on_removal?: boolean;
	backup: MetadataBackupConfig;
	migration?: MetadataMigrationConfig;
}

export interface MetadataMigrationConfig {
	default_group: string;
}
```

- [ ] **Step 6: Verify**

Run: `go build ./... && cd frontend && bun run check`
Expected: both succeed

- [ ] **Step 7: Commit**

```bash
git add internal/config/manager.go internal/config/metadata_migration_test.go frontend/src/types/config.ts
git commit -m "feat(config): add metadata.migration.default_group"
```

---

### Task 3: Scan and group legacy metas

**Files:**
- Create: `internal/metadata/migration_scan.go`
- Create: `internal/metadata/migration_scan_test.go`

**Interfaces:**
- Consumes: `isV3Meta(data []byte) bool` (`service.go:32`), `MetadataService.ReadFileMetadata(virtualPath string) (*metapb.FileMetadata, error)`.
- Produces:
  - `type LegacyMeta struct { MetaPath, VirtualPath string; SizeBytes int64; Meta *metapb.FileMetadata }`
  - `type LegacyGroup struct { Key string; Files []LegacyMeta }`
  - `func (ms *MetadataService) ScanLegacyMetas() ([]LegacyGroup, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/metadata/migration_scan_test.go`:

```go
package metadata

import (
	"os"
	"path/filepath"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeLegacyMeta writes a v1 (inline-segment, no magic) meta at virtualPath.
func writeLegacyMeta(t *testing.T, ms *MetadataService, virtualPath, sourceNzb string, ids ...string) {
	t.Helper()
	segs := make([]*metapb.SegmentData, 0, len(ids))
	for _, id := range ids {
		segs = append(segs, &metapb.SegmentData{Id: id, SegmentSize: 100, StartOffset: 0, EndOffset: 99})
	}
	meta := &metapb.FileMetadata{
		FileSize:      int64(100 * len(ids)),
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		SourceNzbPath: sourceNzb,
		SegmentData:   segs,
	}
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))
}

func TestScanLegacyMetas_GroupsBySourceNzbPath(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)

	writeLegacyMeta(t, ms, filepath.Join("movies", "A.mkv"), "/nzbs/rel.nzb", "a1@n", "a2@n")
	writeLegacyMeta(t, ms, filepath.Join("movies", "B.mkv"), "/nzbs/rel.nzb", "b1@n")
	writeLegacyMeta(t, ms, filepath.Join("other", "C.mkv"), "", "c1@n")

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 2, "two releases: one keyed by source nzb, one by parent dir")

	byKey := map[string][]LegacyMeta{}
	for _, g := range groups {
		byKey[g.Key] = g.Files
	}
	require.Len(t, byKey["/nzbs/rel.nzb"], 2)
	assert.Equal(t, filepath.Join("movies", "A.mkv"), byKey["/nzbs/rel.nzb"][0].VirtualPath)
	assert.Equal(t, filepath.Join("movies", "B.mkv"), byKey["/nzbs/rel.nzb"][1].VirtualPath)

	dirKey := filepath.Join(root, "other")
	require.Len(t, byKey[dirKey], 1, "empty source_nzb_path falls back to the parent directory")
	assert.Positive(t, byKey[dirKey][0].SizeBytes)
	require.Len(t, byKey[dirKey][0].Meta.SegmentData, 1)
}

func TestScanLegacyMetas_SkipsV3Metas(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	storeRef := filepath.Join(t.TempDir(), "rel.nzbz")

	store := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Subject: "V3.mkv", Segments: []*metapb.NzbSeg{{Id: "v1@n", Number: 1, Bytes: 100}}},
	}}
	require.NoError(t, ms.Store().WriteStore(storeRef, store))
	require.NoError(t, ms.WriteFileMetadata(filepath.Join("movies", "V3.mkv"), &metapb.FileMetadata{
		FileSize: 100,
		StoreRef: storeRef,
		SegmentRefs: []*metapb.SegmentRef{
			{StoreIndex: 0, StartOffset: 0, EndOffset: 99, DecodedBytes: 100},
		},
	}))
	writeLegacyMeta(t, ms, filepath.Join("movies", "Old.mkv"), "/nzbs/old.nzb", "o1@n")

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1, "already-migrated metas are skipped")
	assert.Equal(t, "/nzbs/old.nzb", groups[0].Key)

	// Sanity: a non-.meta file in the tree is ignored.
	require.NoError(t, os.WriteFile(filepath.Join(root, "movies", "stray.txt"), []byte("x"), 0644))
	groups, err = ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/ -run TestScanLegacyMetas -v`
Expected: FAIL — `ms.ScanLegacyMetas undefined`

- [ ] **Step 3: Write the implementation**

Create `internal/metadata/migration_scan.go`:

```go
package metadata

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// LegacyMeta is one v1 (inline-segment) metadata file discovered by a scan.
type LegacyMeta struct {
	MetaPath    string
	VirtualPath string
	SizeBytes   int64
	Meta        *metapb.FileMetadata
}

// LegacyGroup is the set of legacy metas belonging to one release. Files are
// sorted by meta path so a migration run is deterministic.
type LegacyGroup struct {
	Key   string
	Files []LegacyMeta
}

// ScanLegacyMetas walks the metadata root and returns every legacy (non-v3)
// meta, grouped by release. The group key is source_nzb_path when set, and the
// meta's parent directory otherwise. Unreadable files are skipped rather than
// failing the whole scan: a migration should convert what it can.
func (ms *MetadataService) ScanLegacyMetas() ([]LegacyGroup, error) {
	byKey := make(map[string][]LegacyMeta)

	err := filepath.WalkDir(ms.rootPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, ".meta") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || isV3Meta(data) {
			return nil
		}
		rel, relErr := filepath.Rel(ms.rootPath, path)
		if relErr != nil {
			return nil
		}
		virtualPath := strings.TrimSuffix(rel, ".meta")

		// Read through the service so the .id sidecar populates NzbdavId.
		meta, metaErr := ms.ReadFileMetadata(virtualPath)
		if metaErr != nil || meta == nil {
			return nil
		}

		key := meta.SourceNzbPath
		if key == "" {
			key = filepath.Dir(path)
		}
		byKey[key] = append(byKey[key], LegacyMeta{
			MetaPath:    path,
			VirtualPath: virtualPath,
			SizeBytes:   int64(len(data)),
			Meta:        meta,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk metadata root: %w", err)
	}

	groups := make([]LegacyGroup, 0, len(byKey))
	for key, files := range byKey {
		sort.Slice(files, func(a, b int) bool { return files[a].MetaPath < files[b].MetaPath })
		groups = append(groups, LegacyGroup{Key: key, Files: files})
	}
	sort.Slice(groups, func(a, b int) bool { return groups[a].Key < groups[b].Key })
	return groups, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metadata/ -run TestScanLegacyMetas -v`
Expected: PASS (both subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/migration_scan.go internal/metadata/migration_scan_test.go
git commit -m "feat(metadata): scan and group legacy metas by release"
```

---

### Task 4: Synthesize a store from inline segments

**Files:**
- Create: `internal/metadata/migration_store.go`
- Create: `internal/metadata/migration_store_test.go`

**Interfaces:**
- Consumes: `LegacyMeta` (Task 3), `ExpandSharedOuterSources(meta *metapb.FileMetadata) error` (`expand.go`).
- Produces:
  - `func synthesizeStore(files []LegacyMeta, defaultGroup string) (*metapb.NzbStore, map[string]int64, error)`
  - `func allSegmentLists(m *metapb.FileMetadata) [][]*metapb.SegmentData`
  - `func storeHashPath(dir, groupKey string, store *metapb.NzbStore) string`

- [ ] **Step 1: Write the failing test**

Create `internal/metadata/migration_store_test.go`:

```go
package metadata

import (
	"path/filepath"
	"strings"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func legacyMeta(virtualPath string, meta *metapb.FileMetadata) LegacyMeta {
	return LegacyMeta{
		MetaPath:    virtualPath + ".meta",
		VirtualPath: virtualPath,
		SizeBytes:   1,
		Meta:        meta,
	}
}

func TestSynthesizeStore_FlatIndexAndDedup(t *testing.T) {
	a := &metapb.FileMetadata{
		CreatedAt: 111,
		SegmentData: []*metapb.SegmentData{
			{Id: "s1@n", SegmentSize: 100, EndOffset: 99},
			{Id: "s2@n", SegmentSize: 100, EndOffset: 99},
		},
		Par2Files: []*metapb.Par2FileReference{
			{Filename: "rel.par2", SegmentData: []*metapb.SegmentData{{Id: "p1@n", SegmentSize: 50, EndOffset: 49}}},
		},
	}
	// b re-uses s2@n (shared outer RAR segment) and adds one of its own.
	b := &metapb.FileMetadata{
		CreatedAt: 222,
		SegmentData: []*metapb.SegmentData{
			{Id: "s2@n", SegmentSize: 100, EndOffset: 99},
			{Id: "s3@n", SegmentSize: 70, EndOffset: 69},
		},
	}

	store, index, err := synthesizeStore(
		[]LegacyMeta{legacyMeta("movies/A.mkv", a), legacyMeta("movies/B.mkv", b)},
		"alt.binaries.misc",
	)
	require.NoError(t, err)
	require.Len(t, store.Files, 2, "one NzbFileEntry per contributing meta")

	assert.Equal(t, "A.mkv", store.Files[0].Subject)
	assert.Equal(t, []string{"alt.binaries.misc"}, store.Files[0].Groups)
	assert.EqualValues(t, 111, store.Files[0].Date)

	// Flat order: s1, s2, p1 (from A), then s3 (from B; s2 already indexed).
	assert.EqualValues(t, 0, index["s1@n"])
	assert.EqualValues(t, 1, index["s2@n"])
	assert.EqualValues(t, 2, index["p1@n"])
	assert.EqualValues(t, 3, index["s3@n"])
	require.Len(t, store.Files[1].Segments, 1, "duplicate ids are not stored twice")
	assert.Equal(t, "s3@n", store.Files[1].Segments[0].Id)

	// Sizes are the decoded sizes from SegmentData.
	assert.EqualValues(t, 100, store.Files[0].Segments[0].Bytes)
	assert.EqualValues(t, 70, store.Files[1].Segments[0].Bytes)

	// Segment numbers are 1-based within their entry.
	assert.EqualValues(t, 1, store.Files[0].Segments[0].Number)
	assert.EqualValues(t, 2, store.Files[0].Segments[1].Number)
}

func TestSynthesizeStore_ExpandsSharedOuterSources(t *testing.T) {
	m := &metapb.FileMetadata{
		SharedOuterSources: []*metapb.NestedSegmentSource{
			{Segments: []*metapb.SegmentData{{Id: "outer@n", SegmentSize: 500, EndOffset: 499}}, InnerVolumeSize: 500},
		},
		NestedSources: []*metapb.NestedSegmentSource{
			{SharedOuterSourceIndex: 1, InnerOffset: 0, InnerLength: 250},
			{SharedOuterSourceIndex: 1, InnerOffset: 250, InnerLength: 250},
		},
	}

	_, index, err := synthesizeStore([]LegacyMeta{legacyMeta("movies/BD.m2ts", m)}, "alt.binaries.misc")
	require.NoError(t, err)
	assert.EqualValues(t, 0, index["outer@n"], "shared outer segments are expanded and indexed")
	assert.Len(t, index, 1)
}

func TestSynthesizeStore_RejectsEmptySegmentID(t *testing.T) {
	m := &metapb.FileMetadata{SegmentData: []*metapb.SegmentData{{Id: "", SegmentSize: 10, EndOffset: 9}}}
	_, _, err := synthesizeStore([]LegacyMeta{legacyMeta("movies/Bad.mkv", m)}, "alt.binaries.misc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty segment id")
}

func TestStoreHashPath_StableAndContentAddressed(t *testing.T) {
	s1 := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Segments: []*metapb.NzbSeg{{Id: "x@n"}, {Id: "y@n"}}},
	}}
	s2 := &metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{Segments: []*metapb.NzbSeg{{Id: "x@n"}}},
	}}

	p1 := storeHashPath("/store", "/nzbs/My Release [2024].nzb", s1)
	assert.Equal(t, p1, storeHashPath("/store", "/nzbs/My Release [2024].nzb", s1), "stable across calls")
	assert.NotEqual(t, p1, storeHashPath("/store", "/nzbs/My Release [2024].nzb", s2), "different segments, different path")

	base := filepath.Base(p1)
	assert.True(t, strings.HasSuffix(base, ".nzbz"))
	assert.True(t, strings.HasPrefix(base, "My_Release__2024_-"), "unsafe characters are replaced, got %q", base)
	assert.NotContains(t, base, "/")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/ -run 'TestSynthesizeStore|TestStoreHashPath' -v`
Expected: FAIL — `undefined: synthesizeStore`

- [ ] **Step 3: Write the implementation**

Create `internal/metadata/migration_store.go`:

```go
package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// allSegmentLists returns every inline SegmentData slice a metadata carries:
// the main file, each PAR2 file, and each nested source. Call
// ExpandSharedOuterSources first so nested sources carry their segments.
func allSegmentLists(m *metapb.FileMetadata) [][]*metapb.SegmentData {
	lists := make([][]*metapb.SegmentData, 0, 1+len(m.Par2Files)+len(m.NestedSources))
	lists = append(lists, m.SegmentData)
	for _, p := range m.Par2Files {
		lists = append(lists, p.SegmentData)
	}
	for _, ns := range m.NestedSources {
		lists = append(lists, ns.Segments)
	}
	return lists
}

// synthesizeStore builds an NzbStore from the inline segments of a legacy
// release group, plus the message-id → flat-index map used to emit SegmentRefs.
//
// Subject/poster/groups are not retained by legacy metas, so entries carry the
// virtual filename as subject and defaultGroup as the single group — enough for
// nzb.BuildNZB to emit a structurally valid NZB. Segment sizes are the decoded
// sizes from SegmentData, which is what segDataToRefs records in decoded_bytes
// and what the read path prefers, so no size information is lost.
//
// One NzbFileEntry per contributing meta keeps each file's segments on a
// contiguous, increasing index range, which is what lets splitRefs collapse
// them into compact SegmentRuns.
func synthesizeStore(files []LegacyMeta, defaultGroup string) (*metapb.NzbStore, map[string]int64, error) {
	store := &metapb.NzbStore{Files: make([]*metapb.NzbFileEntry, 0, len(files))}
	index := make(map[string]int64)
	var flat int64

	var groups []string
	if defaultGroup != "" {
		groups = []string{defaultGroup}
	}

	for _, lm := range files {
		if err := ExpandSharedOuterSources(lm.Meta); err != nil {
			return nil, nil, fmt.Errorf("expand shared outer sources for %s: %w", lm.VirtualPath, err)
		}
		entry := &metapb.NzbFileEntry{
			Subject: filepath.Base(lm.VirtualPath),
			Date:    lm.Meta.CreatedAt,
			Groups:  groups,
		}
		for _, segs := range allSegmentLists(lm.Meta) {
			for _, s := range segs {
				if s.Id == "" {
					return nil, nil, fmt.Errorf("empty segment id in %s", lm.VirtualPath)
				}
				if _, seen := index[s.Id]; seen {
					continue
				}
				index[s.Id] = flat
				flat++
				entry.Segments = append(entry.Segments, &metapb.NzbSeg{
					Id:     s.Id,
					Number: int32(len(entry.Segments) + 1),
					Bytes:  s.SegmentSize,
				})
			}
		}
		store.Files = append(store.Files, entry)
	}
	return store, index, nil
}

// storeHashPath returns the .nzbz path for a group's store. The name embeds the
// first 8 hex characters of the SHA-256 over the store's segment ids in flat
// order, so a store is never overwritten by one holding a different segment set
// — which would silently invalidate the refs of already-migrated metas.
func storeHashPath(dir, groupKey string, store *metapb.NzbStore) string {
	h := sha256.New()
	for _, f := range store.Files {
		for _, s := range f.Segments {
			_, _ = h.Write([]byte(s.Id))
			_, _ = h.Write([]byte{'\n'})
		}
	}
	sum := hex.EncodeToString(h.Sum(nil))[:8]
	return filepath.Join(dir, fmt.Sprintf("%s-%s.nzbz", sanitizeStoreBase(groupKey), sum))
}

// sanitizeStoreBase turns a group key (an nzb path or a directory) into a safe,
// bounded filename component.
func sanitizeStoreBase(groupKey string) string {
	base := filepath.Base(filepath.FromSlash(strings.ReplaceAll(groupKey, `\`, "/")))
	base = strings.TrimSuffix(base, ".nzb")

	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > 80 {
		out = out[:80]
	}
	if out == "" || out == "." || out == ".." {
		out = "release"
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metadata/ -run 'TestSynthesizeStore|TestStoreHashPath' -v`
Expected: PASS (all four tests)

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/migration_store.go internal/metadata/migration_store_test.go
git commit -m "feat(metadata): synthesize NzbStore from legacy inline segments"
```

---

### Task 5: Faithful store from a surviving source NZB

**Files:**
- Modify: `internal/metadata/migration_store.go` (append)
- Modify: `internal/metadata/migration_store_test.go` (append)

**Interfaces:**
- Consumes: `BuildStore` (Task 1), `synthesizeStore`, `allSegmentLists` (Task 4), `LegacyGroup` (Task 3).
- Produces: `func buildGroupStore(g LegacyGroup, defaultGroup string) (*metapb.NzbStore, map[string]int64, bool, error)` — the bool reports whether the faithful path was used.

- [ ] **Step 1: Write the failing test**

Append to `internal/metadata/migration_store_test.go`:

```go
const testNZB = `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <file poster="poster@example.com" date="1700000000" subject="[1/1] &quot;A.mkv&quot; yEnc (1/2)">
    <groups><group>alt.binaries.real</group></groups>
    <segments>
      <segment bytes="140" number="1">s1@n</segment>
      <segment bytes="140" number="2">s2@n</segment>
    </segments>
  </file>
</nzb>
`

func TestBuildGroupStore_FaithfulWhenSourceNzbExists(t *testing.T) {
	dir := t.TempDir()
	nzbPath := filepath.Join(dir, "rel.nzb")
	require.NoError(t, os.WriteFile(nzbPath, []byte(testNZB), 0644))

	g := LegacyGroup{Key: nzbPath, Files: []LegacyMeta{legacyMeta("movies/A.mkv", &metapb.FileMetadata{
		SegmentData: []*metapb.SegmentData{
			{Id: "s1@n", SegmentSize: 100, EndOffset: 99},
			{Id: "s2@n", SegmentSize: 100, EndOffset: 99},
		},
	})}}

	store, index, faithful, err := buildGroupStore(g, "alt.binaries.misc")
	require.NoError(t, err)
	assert.True(t, faithful)
	require.Len(t, store.Files, 1)
	assert.Equal(t, []string{"alt.binaries.real"}, store.Files[0].Groups, "real groups survive")
	assert.Equal(t, "poster@example.com", store.Files[0].Poster)
	assert.EqualValues(t, 0, index["s1@n"])
	assert.EqualValues(t, 1, index["s2@n"])

	// A faithful store regenerates an NZB that parses back with its group intact.
	regenerated := nzb.BuildNZB(store)
	reparsed, parseErr := nzbparser.Parse(bytes.NewReader(regenerated))
	require.NoError(t, parseErr)
	require.Len(t, reparsed.Files, 1)
	assert.Equal(t, []string{"alt.binaries.real"}, reparsed.Files[0].Groups)
	require.Len(t, reparsed.Files[0].Segments, 2)
}

func TestBuildGroupStore_FallsBackWhenNzbMissing(t *testing.T) {
	g := LegacyGroup{Key: "/nzbs/gone.nzb", Files: []LegacyMeta{legacyMeta("movies/A.mkv", &metapb.FileMetadata{
		SegmentData: []*metapb.SegmentData{{Id: "s1@n", SegmentSize: 100, EndOffset: 99}},
	})}}

	store, index, faithful, err := buildGroupStore(g, "alt.binaries.misc")
	require.NoError(t, err)
	assert.False(t, faithful)
	require.Len(t, store.Files, 1)
	assert.Equal(t, []string{"alt.binaries.misc"}, store.Files[0].Groups)
	assert.EqualValues(t, 0, index["s1@n"])
}

func TestBuildGroupStore_FallsBackWhenNzbDoesNotCoverSegments(t *testing.T) {
	dir := t.TempDir()
	nzbPath := filepath.Join(dir, "rel.nzb")
	require.NoError(t, os.WriteFile(nzbPath, []byte(testNZB), 0644))

	// The meta references a segment the NZB does not contain.
	g := LegacyGroup{Key: nzbPath, Files: []LegacyMeta{legacyMeta("movies/A.mkv", &metapb.FileMetadata{
		SegmentData: []*metapb.SegmentData{
			{Id: "s1@n", SegmentSize: 100, EndOffset: 99},
			{Id: "unknown@n", SegmentSize: 100, EndOffset: 99},
		},
	})}}

	store, index, faithful, err := buildGroupStore(g, "alt.binaries.misc")
	require.NoError(t, err)
	assert.False(t, faithful, "a mismatched NZB must not be trusted")
	assert.Equal(t, []string{"alt.binaries.misc"}, store.Files[0].Groups)
	assert.Contains(t, index, "unknown@n")
}
```

Add `"bytes"`, `"os"`, `"github.com/javi11/altmount/internal/nzb"` and `"github.com/javi11/nzbparser"` to the import block of `internal/metadata/migration_store_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/ -run TestBuildGroupStore -v`
Expected: FAIL — `undefined: buildGroupStore`

- [ ] **Step 3: Write the implementation**

Append to `internal/metadata/migration_store.go`:

```go
// buildGroupStore returns the store and flat index for a release group. It
// prefers a faithful store parsed from the surviving source .nzb (real
// subjects, posters, groups and segment numbers) and falls back to synthesis
// from the inline segments. The bool reports which path was taken.
func buildGroupStore(g LegacyGroup, defaultGroup string) (*metapb.NzbStore, map[string]int64, bool, error) {
	if store, index, ok := tryFaithfulStore(g); ok {
		return store, index, true, nil
	}
	store, index, err := synthesizeStore(g.Files, defaultGroup)
	if err != nil {
		return nil, nil, false, err
	}
	return store, index, false, nil
}

// tryFaithfulStore parses the group's source .nzb, if it still exists, and
// returns the resulting store only when it covers every segment referenced by
// every meta in the group. A partial or mismatched NZB (edited, replaced, or
// belonging to a different release) is rejected outright: half a faithful index
// is worse than an honest synthesized one.
func tryFaithfulStore(g LegacyGroup) (*metapb.NzbStore, map[string]int64, bool) {
	if g.Key == "" {
		return nil, nil, false
	}
	f, err := os.Open(g.Key)
	if err != nil {
		return nil, nil, false
	}
	defer func() { _ = f.Close() }()

	parsed, err := nzbparser.Parse(f)
	if err != nil || parsed == nil || len(parsed.Files) == 0 {
		return nil, nil, false
	}
	store, index := BuildStore(parsed)

	for _, lm := range g.Files {
		if expandErr := ExpandSharedOuterSources(lm.Meta); expandErr != nil {
			return nil, nil, false
		}
		for _, segs := range allSegmentLists(lm.Meta) {
			for _, s := range segs {
				if _, ok := index[s.Id]; !ok {
					return nil, nil, false
				}
			}
		}
	}
	return store, index, true
}
```

Add `"os"` and `"github.com/javi11/nzbparser"` to the import block of `internal/metadata/migration_store.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metadata/ -run TestBuildGroupStore -v`
Expected: PASS (all three tests)

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/migration_store.go internal/metadata/migration_store_test.go
git commit -m "feat(metadata): prefer faithful store from surviving source nzb"
```

---

### Task 6: Migrate one group, preserving resolved segments exactly

This task carries the core invariant: after migration, `ReadFileMetadata` must return the same resolved `SegmentData` it returned before.

**Files:**
- Create: `internal/metadata/migration_convert.go`
- Create: `internal/metadata/migration_convert_test.go`

**Interfaces:**
- Consumes: `buildGroupStore` (Task 5), `LegacyGroup`/`LegacyMeta` (Task 3), `MetadataService.Store()`, `MetadataService.WriteFileMetadataV3(ctx, virtualPath, metadata, index, storeRef)`.
- Produces:
  - `type GroupResult struct { Key, StoreRef string; Faithful bool; FilesMigrated, FilesFailed int; BytesBefore, BytesAfter int64; Failures []string }`
  - `func (ms *MetadataService) MigrateGroup(ctx context.Context, g LegacyGroup, storeDir, defaultGroup string) (GroupResult, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/metadata/migration_convert_test.go`:

```go
package metadata

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// readResolved returns the SegmentData a consumer would see for a virtual path.
func readResolved(t *testing.T, ms *MetadataService, virtualPath string) *metapb.FileMetadata {
	t.Helper()
	got, err := ms.ReadFileMetadata(virtualPath)
	require.NoError(t, err)
	require.NotNil(t, got)
	return got
}

func TestMigrateGroup_PreservesResolvedSegments(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)

	pathA := filepath.Join("movies", "A.mkv")
	pathB := filepath.Join("movies", "B.mkv")
	metaA := &metapb.FileMetadata{
		FileSize:      300,
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		SourceNzbPath: "/nzbs/rel.nzb",
		CreatedAt:     10,
		ModifiedAt:    20,
		ReleaseDate:   30,
		KnownHoles:    []*metapb.HoleRun{{StartSegment: 1, Count: 1}},
		SegmentData: []*metapb.SegmentData{
			{Id: "s1@n", SegmentSize: 100, StartOffset: 0, EndOffset: 99},
			{Id: "s2@n", SegmentSize: 100, StartOffset: 0, EndOffset: 99},
			{Id: "s3@n", SegmentSize: 100, StartOffset: 0, EndOffset: 99},
		},
		Par2Files: []*metapb.Par2FileReference{
			{Filename: "rel.par2", FileSize: 50, SegmentData: []*metapb.SegmentData{
				{Id: "p1@n", SegmentSize: 50, StartOffset: 0, EndOffset: 49},
			}},
		},
	}
	// B is archive-sliced: partial offsets that must survive verbatim.
	metaB := &metapb.FileMetadata{
		FileSize:      120,
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		SourceNzbPath: "/nzbs/rel.nzb",
		SegmentData: []*metapb.SegmentData{
			{Id: "s2@n", SegmentSize: 100, StartOffset: 40, EndOffset: 99},
			{Id: "s3@n", SegmentSize: 100, StartOffset: 0, EndOffset: 59},
		},
	}
	require.NoError(t, ms.WriteFileMetadata(pathA, metaA))
	require.NoError(t, ms.WriteFileMetadata(pathB, metaB))

	beforeA := proto.Clone(readResolved(t, ms, pathA)).(*metapb.FileMetadata)
	beforeB := proto.Clone(readResolved(t, ms, pathB)).(*metapb.FileMetadata)

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1)

	res, err := ms.MigrateGroup(context.Background(), groups[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)
	assert.Equal(t, 2, res.FilesMigrated)
	assert.Equal(t, 0, res.FilesFailed)
	assert.False(t, res.Faithful, "no source nzb on disk")
	assert.FileExists(t, res.StoreRef)

	afterA := readResolved(t, ms, pathA)
	afterB := readResolved(t, ms, pathB)

	assert.Equal(t, res.StoreRef, afterA.StoreRef, "meta now points at the shared store")
	require.Len(t, afterA.SegmentData, len(beforeA.SegmentData))
	for i := range beforeA.SegmentData {
		assert.Equal(t, beforeA.SegmentData[i].Id, afterA.SegmentData[i].Id)
		assert.Equal(t, beforeA.SegmentData[i].SegmentSize, afterA.SegmentData[i].SegmentSize)
		assert.Equal(t, beforeA.SegmentData[i].StartOffset, afterA.SegmentData[i].StartOffset)
		assert.Equal(t, beforeA.SegmentData[i].EndOffset, afterA.SegmentData[i].EndOffset)
	}
	require.Len(t, afterA.Par2Files, 1)
	require.Len(t, afterA.Par2Files[0].SegmentData, 1)
	assert.Equal(t, "p1@n", afterA.Par2Files[0].SegmentData[0].Id)

	require.Len(t, afterB.SegmentData, 2)
	assert.EqualValues(t, 40, afterB.SegmentData[0].StartOffset, "archive slicing offsets survive")
	assert.EqualValues(t, 59, afterB.SegmentData[1].EndOffset)

	// Non-segment fields are untouched.
	assert.Equal(t, beforeA.FileSize, afterA.FileSize)
	assert.Equal(t, beforeA.ReleaseDate, afterA.ReleaseDate)
	assert.Equal(t, metapb.FileStatus_FILE_STATUS_HEALTHY, afterA.Status)
	require.Len(t, afterA.KnownHoles, 1)
	assert.EqualValues(t, 1, afterA.KnownHoles[0].StartSegment)
}

func TestMigrateGroup_CompactsIntoSegmentRuns(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)

	segs := make([]*metapb.SegmentData, 0, 10)
	for i := 0; i < 10; i++ {
		segs = append(segs, &metapb.SegmentData{
			Id: string(rune('a'+i)) + "@n", SegmentSize: 100, StartOffset: 0, EndOffset: 99,
		})
	}
	vpath := filepath.Join("movies", "Plain.mkv")
	require.NoError(t, ms.WriteFileMetadata(vpath, &metapb.FileMetadata{
		FileSize: 1000, SourceNzbPath: "/nzbs/plain.nzb", SegmentData: segs,
	}))

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	_, err = ms.MigrateGroup(context.Background(), groups[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)

	// Inspect the raw on-disk proto: a plain file must collapse to runs.
	raw, err := os.ReadFile(filepath.Join(root, "movies", "Plain.mkv.meta"))
	require.NoError(t, err)
	require.True(t, isV3Meta(raw))
	var stored metapb.FileMetadata
	require.NoError(t, proto.Unmarshal(raw[5:], &stored))
	assert.Len(t, stored.SegmentRefs, 0, "no explicit refs for a uniform file")
	require.Len(t, stored.SegmentRuns, 1)
	assert.EqualValues(t, 10, stored.SegmentRuns[0].Count)
	assert.Empty(t, stored.SegmentData, "inline segments are gone")
}

func TestMigrateGroup_IsIdempotentAndSkipsMigratedFiles(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)

	vpath := filepath.Join("movies", "A.mkv")
	require.NoError(t, ms.WriteFileMetadata(vpath, &metapb.FileMetadata{
		FileSize: 100, SourceNzbPath: "/nzbs/rel.nzb",
		SegmentData: []*metapb.SegmentData{{Id: "s1@n", SegmentSize: 100, EndOffset: 99}},
	}))

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	first, err := ms.MigrateGroup(context.Background(), groups[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)

	// A second scan finds nothing, so a second run is a no-op.
	groups, err = ms.ScanLegacyMetas()
	require.NoError(t, err)
	assert.Empty(t, groups, "migrated metas are not rescanned")
	assert.FileExists(t, first.StoreRef)
}
```

Append the reference-count and partial-run tests to the same file:

```go
// countingRefCounter records IncStoreRef calls per store path.
type countingRefCounter struct {
	mu   sync.Mutex
	inc  map[string]int
	decs map[string]int
}

func newCountingRefCounter() *countingRefCounter {
	return &countingRefCounter{inc: map[string]int{}, decs: map[string]int{}}
}

func (c *countingRefCounter) IncStoreRef(_ context.Context, storePath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inc[storePath]++
	return nil
}

func (c *countingRefCounter) DecStoreRef(_ context.Context, storePath string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.decs[storePath]++
	return int64(c.inc[storePath] - c.decs[storePath]), nil
}

func TestMigrateGroup_RefCountMatchesMigratedFiles(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)
	counter := newCountingRefCounter()
	ms.SetStoreRefCounter(counter)

	for _, name := range []string{"A.mkv", "B.mkv", "C.mkv"} {
		require.NoError(t, ms.WriteFileMetadata(filepath.Join("movies", name), &metapb.FileMetadata{
			FileSize: 100, SourceNzbPath: "/nzbs/rel.nzb",
			SegmentData: []*metapb.SegmentData{{Id: name + "@n", SegmentSize: 100, EndOffset: 99}},
		}))
	}

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1)

	res, err := ms.MigrateGroup(context.Background(), groups[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)
	assert.Equal(t, 3, res.FilesMigrated)

	counter.mu.Lock()
	defer counter.mu.Unlock()
	assert.Equal(t, 3, counter.inc[res.StoreRef], "one reference per migrated meta")
}

func TestMigrateGroup_PartialRunLeavesFirstStoreIntact(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)

	pathA := filepath.Join("movies", "A.mkv")
	pathB := filepath.Join("movies", "B.mkv")
	for _, p := range []struct{ path, id string }{{pathA, "a@n"}, {pathB, "b@n"}} {
		require.NoError(t, ms.WriteFileMetadata(p.path, &metapb.FileMetadata{
			FileSize: 100, SourceNzbPath: "/nzbs/rel.nzb",
			SegmentData: []*metapb.SegmentData{{Id: p.id, SegmentSize: 100, EndOffset: 99}},
		}))
	}

	// Simulate a run that only got through the first file: migrate a group
	// containing just A, then rescan and migrate what is left.
	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups[0].Files, 2)

	firstOnly := LegacyGroup{Key: groups[0].Key, Files: groups[0].Files[:1]}
	firstRes, err := ms.MigrateGroup(context.Background(), firstOnly, storeDir, "alt.binaries.misc")
	require.NoError(t, err)
	require.Equal(t, 1, firstRes.FilesMigrated)

	remaining, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Len(t, remaining[0].Files, 1, "only B is left")

	secondRes, err := ms.MigrateGroup(context.Background(), remaining[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)
	assert.Equal(t, 1, secondRes.FilesMigrated)
	assert.NotEqual(t, firstRes.StoreRef, secondRes.StoreRef,
		"a different segment set must not reuse the first store path")
	assert.FileExists(t, firstRes.StoreRef, "the first store is untouched")

	// Both files still resolve correctly, each through its own store.
	gotA := readResolved(t, ms, pathA)
	gotB := readResolved(t, ms, pathB)
	require.Len(t, gotA.SegmentData, 1)
	require.Len(t, gotB.SegmentData, 1)
	assert.Equal(t, "a@n", gotA.SegmentData[0].Id)
	assert.Equal(t, "b@n", gotB.SegmentData[0].Id)
}
```

Add `"sync"` to the import block of `internal/metadata/migration_convert_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/ -run TestMigrateGroup -v`
Expected: FAIL — `ms.MigrateGroup undefined`

- [ ] **Step 3: Write the implementation**

Create `internal/metadata/migration_convert.go`:

```go
package metadata

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// GroupResult reports what happened to one release group.
type GroupResult struct {
	Key           string   `json:"key"`
	StoreRef      string   `json:"store_ref"`
	Faithful      bool     `json:"faithful"`
	FilesMigrated int      `json:"files_migrated"`
	FilesFailed   int      `json:"files_failed"`
	BytesBefore   int64    `json:"bytes_before"`
	BytesAfter    int64    `json:"bytes_after"`
	Failures      []string `json:"failures,omitempty"`
}

// MigrateGroup converts one release group to the v3 store-backed format.
//
// Ordering is load-bearing: the .nzbz is written and read back before any meta
// is repointed at it, so a meta never names a store that is missing or corrupt.
// Each meta is then rewritten atomically by WriteFileMetadataV3, which also
// increments the store's reference count exactly once, leaving the count equal
// to the number of migrated metas.
//
// A per-file failure leaves that file as v1 and is recorded; the rest of the
// group still migrates. If every file fails, the freshly written store is
// removed so no orphan is left behind.
func (ms *MetadataService) MigrateGroup(ctx context.Context, g LegacyGroup, storeDir, defaultGroup string) (GroupResult, error) {
	res := GroupResult{Key: g.Key}
	for _, lm := range g.Files {
		res.BytesBefore += lm.SizeBytes
	}

	store, index, faithful, err := buildGroupStore(g, defaultGroup)
	if err != nil {
		return res, fmt.Errorf("build store for %q: %w", g.Key, err)
	}
	res.Faithful = faithful

	if err := os.MkdirAll(storeDir, 0755); err != nil {
		return res, fmt.Errorf("create store dir %q: %w", storeDir, err)
	}
	storeRef := storeHashPath(storeDir, g.Key, store)
	if err := ms.store.WriteStore(storeRef, store); err != nil {
		return res, fmt.Errorf("write store %q: %w", storeRef, err)
	}
	if _, err := ms.store.ReadStore(storeRef); err != nil {
		_ = os.Remove(storeRef)
		return res, fmt.Errorf("store integrity check failed for %q: %w", storeRef, err)
	}
	res.StoreRef = storeRef

	for _, lm := range g.Files {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return res, ctxErr
		}
		if err := ms.WriteFileMetadataV3(ctx, lm.VirtualPath, lm.Meta, index, storeRef); err != nil {
			res.FilesFailed++
			res.Failures = append(res.Failures, fmt.Sprintf("%s: %v", lm.VirtualPath, err))
			slog.WarnContext(ctx, "Failed to migrate metadata file; leaving it in the legacy format",
				"virtual_path", lm.VirtualPath, "error", err)
			continue
		}
		res.FilesMigrated++
		if info, statErr := os.Stat(lm.MetaPath); statErr == nil {
			res.BytesAfter += info.Size()
		}
	}

	if res.FilesMigrated == 0 {
		if removeErr := os.Remove(storeRef); removeErr != nil && !os.IsNotExist(removeErr) {
			slog.WarnContext(ctx, "Failed to remove orphaned store after a fully failed group",
				"store_ref", storeRef, "error", removeErr)
		}
		res.StoreRef = ""
		return res, nil
	}

	if info, statErr := os.Stat(storeRef); statErr == nil {
		res.BytesAfter += info.Size()
	}
	return res, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metadata/ -run TestMigrateGroup -v`
Expected: PASS (all five tests)

- [ ] **Step 5: Run the whole metadata package to catch regressions**

Run: `go test ./internal/metadata/ 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/metadata/migration_convert.go internal/metadata/migration_convert_test.go
git commit -m "feat(metadata): migrate a release group to the v3 store format"
```

---

### Task 7: The migration worker

**Files:**
- Create: `internal/metadata/migration.go`
- Create: `internal/metadata/migration_test.go`

**Interfaces:**
- Consumes: `MetadataService.ScanLegacyMetas` (Task 3), `MetadataService.MigrateGroup` (Task 6), `config.ConfigGetter`, `cfg.Metadata.Migration.DefaultGroup` (Task 2).
- Produces:
  - `type MigrationProgress`, `type MigrationResult`, `type MigrationStatus`
  - `func NewMigrationWorker(ms *MetadataService, configGetter config.ConfigGetter) *MigrationWorker`
  - `(*MigrationWorker) GetStatus() MigrationStatus`
  - `(*MigrationWorker) Start(ctx context.Context) error`
  - `(*MigrationWorker) DryRun(ctx context.Context) (*MigrationResult, error)`
  - `(*MigrationWorker) Cancel()`

- [ ] **Step 1: Write the failing test**

Create `internal/metadata/migration_test.go`:

```go
package metadata

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfigGetter(t *testing.T) config.ConfigGetter {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "altmount.db")
	cfg := &config.Config{}
	cfg.Database.Path = dbPath
	cfg.Metadata.Migration.DefaultGroup = "alt.binaries.misc"
	return func() *config.Config { return cfg }
}

func seedLegacyRelease(t *testing.T, ms *MetadataService) {
	t.Helper()
	require.NoError(t, ms.WriteFileMetadata(filepath.Join("movies", "A.mkv"), &metapb.FileMetadata{
		FileSize: 200, SourceNzbPath: "/nzbs/rel.nzb",
		SegmentData: []*metapb.SegmentData{
			{Id: "s1@n", SegmentSize: 100, EndOffset: 99},
			{Id: "s2@n", SegmentSize: 100, EndOffset: 99},
		},
	}))
	require.NoError(t, ms.WriteFileMetadata(filepath.Join("movies", "B.mkv"), &metapb.FileMetadata{
		FileSize: 100, SourceNzbPath: "/nzbs/rel.nzb",
		SegmentData: []*metapb.SegmentData{{Id: "s3@n", SegmentSize: 100, EndOffset: 99}},
	}))
}

func TestMigrationWorker_DryRunLeavesLibraryUntouched(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	seedLegacyRelease(t, ms)

	w := NewMigrationWorker(ms, testConfigGetter(t))
	res, err := w.DryRun(context.Background())
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.True(t, res.DryRun)
	assert.Equal(t, 1, res.Groups)
	assert.Equal(t, 2, res.FilesMigrated)
	assert.Equal(t, 0, res.FilesFailed)
	assert.Positive(t, res.BytesBefore)
	assert.Positive(t, res.BytesAfter)

	// The real library is untouched: both metas are still legacy.
	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Len(t, groups[0].Files, 2)

	status := w.GetStatus()
	assert.False(t, status.IsRunning)
	require.NotNil(t, status.LastDryRun)
	assert.Nil(t, status.LastResult, "a dry run is not a migration")
}

func TestMigrationWorker_StartMigratesEverything(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	seedLegacyRelease(t, ms)

	w := NewMigrationWorker(ms, testConfigGetter(t))
	require.NoError(t, w.Start(context.Background()))

	require.Eventually(t, func() bool {
		return !w.GetStatus().IsRunning && w.GetStatus().LastResult != nil
	}, 10*time.Second, 20*time.Millisecond)

	res := w.GetStatus().LastResult
	require.NotNil(t, res)
	assert.False(t, res.DryRun)
	assert.Equal(t, 2, res.FilesMigrated)
	assert.Equal(t, 0, res.FilesFailed)
	assert.Equal(t, 1, res.SynthesizedGroups)
	assert.Equal(t, 0, res.FaithfulGroups)

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	assert.Empty(t, groups, "nothing legacy is left")

	got, err := ms.ReadFileMetadata(filepath.Join("movies", "A.mkv"))
	require.NoError(t, err)
	require.Len(t, got.SegmentData, 2)
	assert.Equal(t, "s1@n", got.SegmentData[0].Id)
}

func TestMigrationWorker_RejectsConcurrentRuns(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	w := NewMigrationWorker(ms, testConfigGetter(t))

	w.mu.Lock()
	w.running = true
	w.mu.Unlock()

	err := w.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	_, dryErr := w.DryRun(context.Background())
	require.Error(t, dryErr)
}
```

`config.ConfigGetter` is `func() *config.Config` (`internal/config/manager.go:1388`), and `Config.Database.Path` / `Config.Metadata.Migration.DefaultGroup` are plain settable fields.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/ -run TestMigrationWorker -v`
Expected: FAIL — `undefined: NewMigrationWorker`

- [ ] **Step 3: Write the implementation**

Create `internal/metadata/migration.go`:

```go
package metadata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
)

// defaultMigrationGroup is used when the config leaves default_group empty.
// Without a group, nzb.BuildNZB emits an empty <groups> element and most NZB
// clients reject the result.
const defaultMigrationGroup = "alt.binaries.misc"

// migrationGroupPause paces the worker so a large library does not saturate
// disk while streams are being served.
const migrationGroupPause = 100 * time.Millisecond

// ErrMigrationRunning is returned when a second run is requested.
var ErrMigrationRunning = errors.New("metadata migration already running")

// MigrationProgress is the live progress of a run.
type MigrationProgress struct {
	TotalGroups     int       `json:"total_groups"`
	ProcessedGroups int       `json:"processed_groups"`
	TotalFiles      int       `json:"total_files"`
	ProcessedFiles  int       `json:"processed_files"`
	CurrentRelease  string    `json:"current_release"`
	StartTime       time.Time `json:"start_time"`
}

// MigrationResult summarises a completed run (real or dry).
type MigrationResult struct {
	DryRun            bool          `json:"dry_run"`
	Groups            int           `json:"groups"`
	FaithfulGroups    int           `json:"faithful_groups"`
	SynthesizedGroups int           `json:"synthesized_groups"`
	FilesMigrated     int           `json:"files_migrated"`
	FilesFailed       int           `json:"files_failed"`
	BytesBefore       int64         `json:"bytes_before"`
	BytesAfter        int64         `json:"bytes_after"`
	BytesSaved        int64         `json:"bytes_saved"`
	Failures          []string      `json:"failures,omitempty"`
	Cancelled         bool          `json:"cancelled"`
	Duration          time.Duration `json:"duration"`
	CompletedAt       time.Time     `json:"completed_at"`
}

// MigrationStatus is what the API reports.
type MigrationStatus struct {
	IsRunning    bool               `json:"is_running"`
	LegacyFiles  int                `json:"legacy_files"`
	LegacyGroups int                `json:"legacy_groups"`
	Progress     *MigrationProgress `json:"progress,omitempty"`
	LastResult   *MigrationResult   `json:"last_result,omitempty"`
	LastDryRun   *MigrationResult   `json:"last_dry_run,omitempty"`
}

// MigrationWorker converts legacy inline-segment metadata to the v3
// store-backed format. It is manually triggered; nothing runs on startup.
type MigrationWorker struct {
	ms           *MetadataService
	configGetter config.ConfigGetter

	mu         sync.Mutex
	running    bool
	cancelFunc context.CancelFunc

	progressMu sync.RWMutex
	progress   *MigrationProgress
	lastResult *MigrationResult
	lastDryRun *MigrationResult
}

// NewMigrationWorker creates a migration worker. It does not start anything.
func NewMigrationWorker(ms *MetadataService, configGetter config.ConfigGetter) *MigrationWorker {
	return &MigrationWorker{ms: ms, configGetter: configGetter}
}

// GetStatus reports whether a run is active, its progress, and the last results.
// The legacy counts come from the live progress when running and are otherwise
// left at zero; callers wanting a fresh count run a dry run.
func (w *MigrationWorker) GetStatus() MigrationStatus {
	w.mu.Lock()
	running := w.running
	w.mu.Unlock()

	w.progressMu.RLock()
	defer w.progressMu.RUnlock()

	status := MigrationStatus{
		IsRunning:  running,
		LastResult: w.lastResult,
		LastDryRun: w.lastDryRun,
	}
	if w.progress != nil {
		p := *w.progress
		status.Progress = &p
		status.LegacyFiles = p.TotalFiles
		status.LegacyGroups = p.TotalGroups
	}
	return status
}

// Cancel stops an in-flight run after the current file.
func (w *MigrationWorker) Cancel() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancelFunc != nil {
		w.cancelFunc()
	}
}

// Start launches a migration in the background and returns immediately.
// The run is detached from the caller's context so cancelling an HTTP request
// does not abort a migration; use Cancel for that.
func (w *MigrationWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return ErrMigrationRunning
	}
	runCtx, cancel := context.WithCancel(context.Background())
	w.running = true
	w.cancelFunc = cancel
	w.mu.Unlock()

	slog.InfoContext(ctx, "Starting metadata migration to v3 store format")

	go func() {
		defer func() {
			w.mu.Lock()
			w.running = false
			w.cancelFunc = nil
			w.mu.Unlock()
			cancel()
		}()

		result, err := w.run(runCtx, false)
		if err != nil {
			slog.ErrorContext(runCtx, "Metadata migration failed", "error", err)
			return
		}
		w.progressMu.Lock()
		w.lastResult = result
		w.progressMu.Unlock()
		slog.InfoContext(runCtx, "Metadata migration finished",
			"files_migrated", result.FilesMigrated,
			"files_failed", result.FilesFailed,
			"bytes_saved", result.BytesSaved,
			"cancelled", result.Cancelled)
	}()
	return nil
}

// DryRun performs the real conversion against an isolated temporary metadata
// root, measures the result, and deletes it. Nothing in the library is touched
// and no store reference counts move (the temporary service has no counter).
func (w *MigrationWorker) DryRun(ctx context.Context) (*MigrationResult, error) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil, ErrMigrationRunning
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.running = true
	w.cancelFunc = cancel
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.running = false
		w.cancelFunc = nil
		w.mu.Unlock()
		cancel()
	}()

	result, err := w.run(runCtx, true)
	if err != nil {
		return nil, err
	}
	w.progressMu.Lock()
	w.lastDryRun = result
	w.progressMu.Unlock()
	return result, nil
}

// run is the shared body of Start and DryRun.
func (w *MigrationWorker) run(ctx context.Context, dryRun bool) (*MigrationResult, error) {
	started := time.Now()

	groups, err := w.ms.ScanLegacyMetas()
	if err != nil {
		return nil, fmt.Errorf("scan legacy metadata: %w", err)
	}

	totalFiles := 0
	for _, g := range groups {
		totalFiles += len(g.Files)
	}
	w.setProgress(&MigrationProgress{
		TotalGroups: len(groups),
		TotalFiles:  totalFiles,
		StartTime:   started,
	})

	target := w.ms
	storeDir := w.storeDir()
	if dryRun {
		tmpRoot, tmpErr := os.MkdirTemp("", "altmount-migration-dryrun-")
		if tmpErr != nil {
			return nil, fmt.Errorf("create dry-run temp root: %w", tmpErr)
		}
		defer func() { _ = os.RemoveAll(tmpRoot) }()
		target = NewMetadataService(filepath.Join(tmpRoot, "meta"))
		storeDir = filepath.Join(tmpRoot, "store")
	}

	result := &MigrationResult{DryRun: dryRun, Groups: len(groups)}
	defaultGroup := w.defaultGroup()

	for _, g := range groups {
		if ctx.Err() != nil {
			result.Cancelled = true
			break
		}
		w.updateProgressGroup(g.Key)

		gr, groupErr := target.MigrateGroup(ctx, g, storeDir, defaultGroup)
		if groupErr != nil {
			if ctx.Err() != nil {
				result.Cancelled = true
				break
			}
			result.FilesFailed += len(g.Files)
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", g.Key, groupErr))
			slog.WarnContext(ctx, "Skipping release group that failed to migrate",
				"group", g.Key, "error", groupErr)
			continue
		}

		if gr.Faithful {
			result.FaithfulGroups++
		} else {
			result.SynthesizedGroups++
		}
		result.FilesMigrated += gr.FilesMigrated
		result.FilesFailed += gr.FilesFailed
		result.BytesBefore += gr.BytesBefore
		result.BytesAfter += gr.BytesAfter
		result.Failures = append(result.Failures, gr.Failures...)

		w.advanceProgress(len(g.Files))
		if !dryRun {
			select {
			case <-ctx.Done():
				result.Cancelled = true
			case <-time.After(migrationGroupPause):
			}
		}
	}

	result.BytesSaved = result.BytesBefore - result.BytesAfter
	result.Duration = time.Since(started)
	result.CompletedAt = time.Now()
	w.setProgress(nil)
	return result, nil
}

// storeDir is where migrated .nzbz files live: <configDir>/.nzbs/_migrated,
// mirroring the importer's layout.
func (w *MigrationWorker) storeDir() string {
	cfg := w.configGetter()
	configDir := filepath.Dir(cfg.Database.Path)
	if !filepath.IsAbs(configDir) {
		if abs, err := filepath.Abs(configDir); err == nil {
			configDir = abs
		}
	}
	return filepath.Join(configDir, ".nzbs", "_migrated")
}

func (w *MigrationWorker) defaultGroup() string {
	if g := w.configGetter().Metadata.Migration.DefaultGroup; g != "" {
		return g
	}
	return defaultMigrationGroup
}

func (w *MigrationWorker) setProgress(p *MigrationProgress) {
	w.progressMu.Lock()
	defer w.progressMu.Unlock()
	w.progress = p
}

func (w *MigrationWorker) updateProgressGroup(key string) {
	w.progressMu.Lock()
	defer w.progressMu.Unlock()
	if w.progress != nil {
		w.progress.CurrentRelease = key
	}
}

func (w *MigrationWorker) advanceProgress(files int) {
	w.progressMu.Lock()
	defer w.progressMu.Unlock()
	if w.progress != nil {
		w.progress.ProcessedGroups++
		w.progress.ProcessedFiles += files
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metadata/ -run TestMigrationWorker -v`
Expected: PASS (all three tests)

- [ ] **Step 5: Run the full package and build**

Run: `go build ./... && go test ./internal/metadata/ 2>&1 | tail -20`
Expected: build succeeds, tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/metadata/migration.go internal/metadata/migration_test.go
git commit -m "feat(metadata): add manually-triggered v3 migration worker"
```

---

### Task 8: API endpoints and wiring

**Files:**
- Create: `internal/api/metadata_migration_handlers.go`
- Modify: `internal/api/server.go` — add the field near line 56, a setter near line 145, routes near line 296, and the four `Server` methods near line 498
- Modify: `cmd/altmount/cmd/serve.go` near line 207

**Interfaces:**
- Consumes: everything from Task 7.
- Produces: `(*Server).SetMetadataMigrationWorker(w *metadata.MigrationWorker)` and the four routes.

- [ ] **Step 1: Write the handlers**

Create `internal/api/metadata_migration_handlers.go`:

```go
package api

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/metadata"
)

// MetadataMigrationHandlers holds the legacy-metadata migration handlers.
type MetadataMigrationHandlers struct {
	worker *metadata.MigrationWorker
}

// NewMetadataMigrationHandlers creates a new instance of the migration handlers.
func NewMetadataMigrationHandlers(worker *metadata.MigrationWorker) *MetadataMigrationHandlers {
	return &MetadataMigrationHandlers{worker: worker}
}

// handleGetStatus handles GET /api/metadata/migration/status
func (h *MetadataMigrationHandlers) handleGetStatus(c *fiber.Ctx) error {
	return RespondSuccess(c, h.worker.GetStatus())
}

// handleDryRun handles POST /api/metadata/migration/dry-run
func (h *MetadataMigrationHandlers) handleDryRun(c *fiber.Ctx) error {
	result, err := h.worker.DryRun(c.Context())
	if err != nil {
		if errors.Is(err, metadata.ErrMigrationRunning) {
			return RespondConflict(c, "Metadata migration already running", err.Error())
		}
		slog.ErrorContext(c.Context(), "Metadata migration dry run failed", "error", err)
		return RespondInternalError(c, "Failed to perform migration dry run", err.Error())
	}
	return RespondSuccess(c, result)
}

// handleStart handles POST /api/metadata/migration/start
func (h *MetadataMigrationHandlers) handleStart(c *fiber.Ctx) error {
	if err := h.worker.Start(c.Context()); err != nil {
		if errors.Is(err, metadata.ErrMigrationRunning) {
			return RespondConflict(c, "Metadata migration already running", err.Error())
		}
		slog.ErrorContext(c.Context(), "Failed to start metadata migration", "error", err)
		return RespondInternalError(c, "Failed to start metadata migration", err.Error())
	}
	return RespondMessage(c, "Metadata migration started")
}

// handleCancel handles POST /api/metadata/migration/cancel
func (h *MetadataMigrationHandlers) handleCancel(c *fiber.Ctx) error {
	h.worker.Cancel()
	return RespondMessage(c, "Metadata migration cancelled")
}
```

- [ ] **Step 2: Wire the server**

In `internal/api/server.go`, add the field to the `Server` struct next to `librarySyncWorker` (line 56):

```go
	metadataMigrationWorker *metadata.MigrationWorker
```

Add the setter after `SetLibrarySyncWorker` (line 147):

```go
// SetMetadataMigrationWorker sets the metadata migration worker for the server.
func (s *Server) SetMetadataMigrationWorker(worker *metadata.MigrationWorker) {
	s.metadataMigrationWorker = worker
}
```

Add the routes next to the library-sync routes (after line 296):

```go
	api.Get("/metadata/migration/status", s.handleGetMetadataMigrationStatus)
	api.Post("/metadata/migration/dry-run", s.handleDryRunMetadataMigration)
	api.Post("/metadata/migration/start", s.handleStartMetadataMigration)
	api.Post("/metadata/migration/cancel", s.handleCancelMetadataMigration)
```

Add the four `Server` methods near the other library-sync `Server` methods (after line 520):

```go
// Metadata migration handler methods

// handleGetMetadataMigrationStatus handles GET /api/metadata/migration/status
//
//	@Summary		Get metadata migration status
//	@Description	Returns the status of the legacy metadata → v3 migration worker.
//	@Tags			Metadata
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Failure		503	{object}	APIResponse
//	@Security		BearerAuth
//	@Router			/metadata/migration/status [get]
func (s *Server) handleGetMetadataMigrationStatus(c *fiber.Ctx) error {
	if s.metadataMigrationWorker == nil {
		return RespondServiceUnavailable(c, "Metadata migration worker not available", "")
	}
	return NewMetadataMigrationHandlers(s.metadataMigrationWorker).handleGetStatus(c)
}

// handleDryRunMetadataMigration handles POST /api/metadata/migration/dry-run
//
//	@Summary		Dry-run the metadata migration
//	@Description	Converts against an isolated temporary root and reports measured savings without touching the library.
//	@Tags			Metadata
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Failure		409	{object}	APIResponse
//	@Failure		503	{object}	APIResponse
//	@Security		BearerAuth
//	@Router			/metadata/migration/dry-run [post]
func (s *Server) handleDryRunMetadataMigration(c *fiber.Ctx) error {
	if s.metadataMigrationWorker == nil {
		return RespondServiceUnavailable(c, "Metadata migration worker not available", "")
	}
	return NewMetadataMigrationHandlers(s.metadataMigrationWorker).handleDryRun(c)
}

// handleStartMetadataMigration handles POST /api/metadata/migration/start
//
//	@Summary		Start the metadata migration
//	@Description	Rewrites legacy .meta files to the v3 store-backed format in place.
//	@Tags			Metadata
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Failure		409	{object}	APIResponse
//	@Failure		503	{object}	APIResponse
//	@Security		BearerAuth
//	@Router			/metadata/migration/start [post]
func (s *Server) handleStartMetadataMigration(c *fiber.Ctx) error {
	if s.metadataMigrationWorker == nil {
		return RespondServiceUnavailable(c, "Metadata migration worker not available", "")
	}
	return NewMetadataMigrationHandlers(s.metadataMigrationWorker).handleStart(c)
}

// handleCancelMetadataMigration handles POST /api/metadata/migration/cancel
//
//	@Summary		Cancel the metadata migration
//	@Description	Stops an in-flight migration after the current file.
//	@Tags			Metadata
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Failure		503	{object}	APIResponse
//	@Security		BearerAuth
//	@Router			/metadata/migration/cancel [post]
func (s *Server) handleCancelMetadataMigration(c *fiber.Ctx) error {
	if s.metadataMigrationWorker == nil {
		return RespondServiceUnavailable(c, "Metadata migration worker not available", "")
	}
	return NewMetadataMigrationHandlers(s.metadataMigrationWorker).handleCancel(c)
}
```

`internal/api/server.go` already imports `github.com/javi11/altmount/internal/metadata` (it holds `metadataService *metadata.MetadataService`); confirm with `grep -n "internal/metadata" internal/api/server.go` and add the import only if it is missing.

- [ ] **Step 3: Wire it in serve.go**

In `cmd/altmount/cmd/serve.go`, after the `apiServer.SetLibrarySyncWorker(librarySyncWorker)` block (line 205-209), add:

```go
	apiServer.SetMetadataMigrationWorker(
		metadata.NewMigrationWorker(metadataService, configManager.GetConfigGetter()),
	)
```

`metadataService` is already in scope from line 87. Add `"github.com/javi11/altmount/internal/metadata"` to the imports of `serve.go` if it is not already there — check with `grep -n "internal/metadata" cmd/altmount/cmd/serve.go`.

- [ ] **Step 4: Verify the build and the API package**

Run: `go build ./... && go vet ./internal/api/ ./cmd/... && go test ./internal/api/ 2>&1 | tail -20`
Expected: build and vet clean, API tests PASS

- [ ] **Step 5: Smoke-test the routes are registered**

Run: `grep -n "metadata/migration" internal/api/server.go`
Expected: four lines, one per route

- [ ] **Step 6: Commit**

```bash
git add internal/api/metadata_migration_handlers.go internal/api/server.go cmd/altmount/cmd/serve.go
git commit -m "feat(api): expose metadata migration status, dry-run, start and cancel"
```

---

### Task 9: Frontend types, API client, and hook

**Files:**
- Modify: `frontend/src/types/api.ts` (add next to `LibrarySyncStatus`)
- Modify: `frontend/src/api/client.ts` (add after `cancelLibrarySync`, around line 508)
- Create: `frontend/src/hooks/useMetadataMigration.ts`

**Interfaces:**
- Consumes: the four endpoints from Task 8.
- Produces: `MetadataMigrationStatus`, `MetadataMigrationResult`, `MetadataMigrationProgress` types; `apiClient.getMetadataMigrationStatus/dryRunMetadataMigration/startMetadataMigration/cancelMetadataMigration`; hooks `useMetadataMigrationStatus`, `useDryRunMetadataMigration`, `useStartMetadataMigration`, `useCancelMetadataMigration`.

- [ ] **Step 1: Add the types**

In `frontend/src/types/api.ts`, next to the existing `LibrarySyncStatus` interface (find it with `grep -n "LibrarySyncStatus" frontend/src/types/api.ts`), add:

```ts
export interface MetadataMigrationProgress {
	total_groups: number;
	processed_groups: number;
	total_files: number;
	processed_files: number;
	current_release: string;
	start_time: string;
}

export interface MetadataMigrationResult {
	dry_run: boolean;
	groups: number;
	faithful_groups: number;
	synthesized_groups: number;
	files_migrated: number;
	files_failed: number;
	bytes_before: number;
	bytes_after: number;
	bytes_saved: number;
	failures?: string[];
	cancelled: boolean;
	duration: number;
	completed_at: string;
}

export interface MetadataMigrationStatus {
	is_running: boolean;
	legacy_files: number;
	legacy_groups: number;
	progress?: MetadataMigrationProgress;
	last_result?: MetadataMigrationResult;
	last_dry_run?: MetadataMigrationResult;
}
```

- [ ] **Step 2: Add the client methods**

In `frontend/src/api/client.ts`, after `cancelLibrarySync()` (ends around line 508), add:

```ts
	async getMetadataMigrationStatus() {
		return this.request<MetadataMigrationStatus>("/metadata/migration/status");
	}

	async dryRunMetadataMigration() {
		return this.request<MetadataMigrationResult>("/metadata/migration/dry-run", {
			method: "POST",
		});
	}

	async startMetadataMigration() {
		return this.request<{ message: string }>("/metadata/migration/start", {
			method: "POST",
		});
	}

	async cancelMetadataMigration() {
		return this.request<{ message: string }>("/metadata/migration/cancel", {
			method: "POST",
		});
	}
```

Add `MetadataMigrationResult` and `MetadataMigrationStatus` to the existing type import from `../types/api` at the top of `client.ts` (match the file's existing import style, which the `LibrarySyncStatus` import already demonstrates).

- [ ] **Step 3: Write the hook**

Create `frontend/src/hooks/useMetadataMigration.ts`:

```ts
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../api/client";
import type { MetadataMigrationResult, MetadataMigrationStatus } from "../types/api";

const MIGRATION_KEY = ["metadata", "migration"];

// Hook to poll migration status: fast while running, slow when idle.
export function useMetadataMigrationStatus() {
	return useQuery<MetadataMigrationStatus>({
		queryKey: [...MIGRATION_KEY, "status"],
		queryFn: () => apiClient.getMetadataMigrationStatus(),
		retry: 3,
		refetchInterval: (query) => {
			if (query.state.error) return false;
			if (!query.state.data) return 10000;
			return query.state.data.is_running ? 2000 : 10000;
		},
		refetchIntervalInBackground: true,
	});
}

// Hook to run a dry run. Resolves with the measured result.
export function useDryRunMetadataMigration() {
	const queryClient = useQueryClient();

	return useMutation<MetadataMigrationResult>({
		mutationFn: () => apiClient.dryRunMetadataMigration(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: MIGRATION_KEY });
		},
		onError: (error) => {
			console.error("Metadata migration dry run failed:", error);
		},
	});
}

// Hook to start the migration.
export function useStartMetadataMigration() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.startMetadataMigration(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: MIGRATION_KEY });
		},
		onError: (error) => {
			console.error("Failed to start metadata migration:", error);
		},
	});
}

// Hook to cancel an in-flight migration.
export function useCancelMetadataMigration() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.cancelMetadataMigration(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: MIGRATION_KEY });
		},
		onError: (error) => {
			console.error("Failed to cancel metadata migration:", error);
		},
	});
}
```

- [ ] **Step 4: Verify**

Run: `cd frontend && bun run check`
Expected: PASS with no type or lint errors

- [ ] **Step 5: Commit**

```bash
git add frontend/src/types/api.ts frontend/src/api/client.ts frontend/src/hooks/useMetadataMigration.ts
git commit -m "feat(frontend): add metadata migration api client and hooks"
```

---

### Task 10: The migration card in the metadata config section

**Files:**
- Create: `frontend/src/components/config/MetadataMigrationCard.tsx`
- Modify: `frontend/src/components/config/MetadataConfigSection.tsx`

**Interfaces:**
- Consumes: the hooks from Task 9.
- Produces: `MetadataMigrationCard` (named export, no props).

- [ ] **Step 1: Write the component**

Create `frontend/src/components/config/MetadataMigrationCard.tsx`:

```tsx
import { AlertTriangle, Archive, CheckCircle, Play, Search, XCircle } from "lucide-react";
import { useState } from "react";
import {
	useCancelMetadataMigration,
	useDryRunMetadataMigration,
	useMetadataMigrationStatus,
	useStartMetadataMigration,
} from "../../hooks/useMetadataMigration";
import { LoadingSpinner } from "../ui/LoadingSpinner";

function formatBytes(bytes: number): string {
	if (bytes <= 0) return "0 B";
	const units = ["B", "KB", "MB", "GB", "TB"];
	const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
	return `${(bytes / 1024 ** exponent).toFixed(exponent === 0 ? 0 : 1)} ${units[exponent]}`;
}

export function MetadataMigrationCard() {
	const { data: status, isLoading } = useMetadataMigrationStatus();
	const dryRun = useDryRunMetadataMigration();
	const startMigration = useStartMetadataMigration();
	const cancelMigration = useCancelMetadataMigration();
	const [showConfirm, setShowConfirm] = useState(false);

	const isRunning = status?.is_running ?? false;
	const preview = status?.last_dry_run ?? dryRun.data;
	const lastResult = status?.last_result;
	const progress = status?.progress;
	const percent =
		progress && progress.total_files > 0
			? Math.round((progress.processed_files / progress.total_files) * 100)
			: 0;

	const handleConfirmMigrate = async () => {
		setShowConfirm(false);
		await startMigration.mutateAsync();
	};

	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body">
				<h2 className="card-title">
					<Archive className="h-5 w-5" aria-hidden="true" />
					Storage Migration
				</h2>
				<p className="text-base-content/70 text-sm">
					Convert legacy metadata files to the shared NZB store format. Segments move out of each
					<code className="mx-1">.meta</code> file into one compressed
					<code className="mx-1">.nzbz</code> per release, reclaiming disk space.
				</p>

				{isLoading ? (
					<div className="flex justify-center py-6">
						<LoadingSpinner size="md" />
					</div>
				) : (
					<>
						{isRunning && progress && (
							<div className="space-y-2 py-2">
								<div className="flex justify-between text-sm">
									<span>
										Release {progress.processed_groups} of {progress.total_groups}
									</span>
									<span>
										{progress.processed_files} / {progress.total_files} files
									</span>
								</div>
								<progress className="progress progress-primary w-full" value={percent} max={100} />
								{progress.current_release && (
									<p className="truncate text-base-content/60 text-xs">
										{progress.current_release}
									</p>
								)}
							</div>
						)}

						{!isRunning && preview && (
							<div className="overflow-x-auto py-2">
								<table className="table table-sm">
									<tbody>
										<tr>
											<td>Releases</td>
											<td className="text-right font-mono">{preview.groups}</td>
										</tr>
										<tr>
											<td>Files to convert</td>
											<td className="text-right font-mono">{preview.files_migrated}</td>
										</tr>
										<tr>
											<td>Current size</td>
											<td className="text-right font-mono">{formatBytes(preview.bytes_before)}</td>
										</tr>
										<tr>
											<td>Projected size</td>
											<td className="text-right font-mono">{formatBytes(preview.bytes_after)}</td>
										</tr>
										<tr className="font-semibold">
											<td>Reclaimed</td>
											<td className="text-right font-mono text-success">
												{formatBytes(preview.bytes_saved)}
											</td>
										</tr>
										{preview.files_failed > 0 && (
											<tr>
												<td>Cannot convert</td>
												<td className="text-right font-mono text-warning">
													{preview.files_failed}
												</td>
											</tr>
										)}
									</tbody>
								</table>
							</div>
						)}

						{!isRunning && preview && preview.groups === 0 && (
							<div className="alert alert-success">
								<CheckCircle className="h-6 w-6" aria-hidden="true" />
								<div>All metadata is already in the shared store format.</div>
							</div>
						)}

						{lastResult && !isRunning && (
							<div className={`alert ${lastResult.files_failed > 0 ? "alert-warning" : "alert-success"}`}>
								{lastResult.files_failed > 0 ? (
									<AlertTriangle className="h-6 w-6" aria-hidden="true" />
								) : (
									<CheckCircle className="h-6 w-6" aria-hidden="true" />
								)}
								<div>
									<div className="font-bold">
										{lastResult.cancelled ? "Migration cancelled" : "Migration complete"}
									</div>
									<div className="text-sm">
										{lastResult.files_migrated} files migrated,{" "}
										{formatBytes(lastResult.bytes_saved)} reclaimed
										{lastResult.files_failed > 0 && `, ${lastResult.files_failed} skipped`}
									</div>
								</div>
							</div>
						)}

						{dryRun.isError && (
							<div className="alert alert-error">
								<XCircle className="h-6 w-6" aria-hidden="true" />
								<div>Dry run failed. Check the logs for details.</div>
							</div>
						)}

						<div className="card-actions justify-end pt-2">
							{isRunning ? (
								<button
									type="button"
									className="btn btn-error btn-outline"
									onClick={() => cancelMigration.mutate()}
									disabled={cancelMigration.isPending}
								>
									<XCircle className="h-4 w-4" aria-hidden="true" />
									Cancel
								</button>
							) : (
								<>
									<button
										type="button"
										className="btn btn-outline"
										onClick={() => dryRun.mutate()}
										disabled={dryRun.isPending}
									>
										{dryRun.isPending ? (
											<LoadingSpinner size="sm" />
										) : (
											<Search className="h-4 w-4" aria-hidden="true" />
										)}
										{dryRun.isPending ? "Scanning..." : "Dry Run"}
									</button>
									<button
										type="button"
										className="btn btn-primary"
										onClick={() => setShowConfirm(true)}
										disabled={!preview || preview.groups === 0 || startMigration.isPending}
									>
										<Play className="h-4 w-4" aria-hidden="true" />
										Migrate
									</button>
								</>
							)}
						</div>
					</>
				)}
			</div>

			{showConfirm && (
				<dialog className="modal modal-open">
					<div className="modal-box">
						<h3 className="font-bold text-lg">Migrate metadata?</h3>
						<p className="py-4">
							This rewrites every legacy <code>.meta</code> file in place and moves its segments into
							a shared <code>.nzbz</code> store per release. The change is one-way. Streaming keeps
							working throughout, and you can cancel at any point.
						</p>
						<div className="modal-action">
							<button type="button" className="btn" onClick={() => setShowConfirm(false)}>
								Cancel
							</button>
							<button type="button" className="btn btn-primary" onClick={handleConfirmMigrate}>
								Migrate
							</button>
						</div>
					</div>
				</dialog>
			)}
		</div>
	);
}
```

- [ ] **Step 2: Render it in the metadata section**

In `frontend/src/components/config/MetadataConfigSection.tsx`, add the import next to the existing component imports:

```tsx
import { MetadataMigrationCard } from "./MetadataMigrationCard";
```

Then render it just above the `{/* Save Button */}` block at the end of the returned JSX:

```tsx
			<MetadataMigrationCard />

			{/* Save Button */}
```

- [ ] **Step 3: Verify**

Run: `cd frontend && bun run check && bun run build`
Expected: both PASS

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/config/MetadataMigrationCard.tsx frontend/src/components/config/MetadataConfigSection.tsx
git commit -m "feat(frontend): add storage migration card to metadata settings"
```

---

### Task 11: Full verification

**Files:** none created; this validates the whole change.

**Interfaces:**
- Consumes: everything.
- Produces: a green build.

- [ ] **Step 1: Run the full Go test suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v "^ok\|no test files" | head -30`
Expected: no FAIL lines

- [ ] **Step 2: Run the full project build**

Run: `make`
Expected: succeeds (Go backend + frontend, all validations)

- [ ] **Step 3: Confirm the migration round-trips on a realistic tree**

Run: `go test ./internal/metadata/ -run 'TestMigrate|TestScanLegacy|TestSynthesize|TestBuildGroupStore|TestMigrationWorker' -v 2>&1 | tail -30`
Expected: all PASS

- [ ] **Step 4: Commit any formatting fixes the build produced**

```bash
git status --short
git add -A
git commit -m "chore: formatting and generated output after metadata migration feature"
```

Skip this commit if `git status --short` is empty.
