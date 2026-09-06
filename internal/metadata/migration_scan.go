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
//
// Meta is deliberately nil after a scan: a legacy meta carries its segments
// inline, so retaining every one of them across a whole-library walk is the
// allocation pattern that ReadFileMetadataLite exists to avoid (see the 7.94 GB
// spike documented at service.go:405). LoadGroupMetas fills it in for one
// release at a time, just before that release is migrated.
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
//
// Each meta is read to obtain its group key and then released, so the walk
// retains only paths and sizes — peak memory is one meta, not the library.
// Call LoadGroupMetas to hydrate a single group before migrating it.
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
		// meta goes out of scope here on purpose — see the LegacyMeta doc.
		byKey[key] = append(byKey[key], LegacyMeta{
			MetaPath:    path,
			VirtualPath: virtualPath,
			SizeBytes:   int64(len(data)),
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

// LoadGroupMetas returns a copy of g with every LegacyMeta.Meta populated.
// Files that can no longer be read (deleted or migrated since the scan) are
// dropped, so a stale scan degrades to a smaller group rather than an error.
func (ms *MetadataService) LoadGroupMetas(g LegacyGroup) (LegacyGroup, error) {
	loaded := LegacyGroup{Key: g.Key, Files: make([]LegacyMeta, 0, len(g.Files))}
	for _, lm := range g.Files {
		meta, err := ms.ReadFileMetadata(lm.VirtualPath)
		if err != nil || meta == nil {
			continue
		}
		lm.Meta = meta
		loaded.Files = append(loaded.Files, lm)
	}
	if len(loaded.Files) == 0 {
		return loaded, fmt.Errorf("no readable metadata left in group %q", g.Key)
	}
	return loaded, nil
}
