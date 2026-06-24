package sharenet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ReleaseStore maps a releaseHash to the ordered set of .meta virtual paths that
// a single NZB produced (one for a single-file release, many for a multi-file or
// archive release — all sharing one rebuilt-locally NzbStore). It reads the raw
// on-disk v3 .meta bytes the handler serves to peers.
//
// Only the per-file .meta is shared; the segment data lives in the release's
// .nzbz store, which every node rebuilds deterministically from the same NZB.
//
// virtualPath is relative to metadataRoot, e.g. "ShowName/episode.s01e01" which
// maps to "{metadataRoot}/ShowName/episode.s01e01.meta".
type ReleaseStore struct {
	mu           sync.RWMutex
	metadataRoot string
	releases     map[string][]string // releaseHash → ordered virtual paths
}

// NewReleaseStore creates a store rooted at metadataRoot.
// metadataRoot should be the same as config.Metadata.RootPath.
func NewReleaseStore(metadataRoot string) *ReleaseStore {
	return &ReleaseStore{
		metadataRoot: filepath.Clean(metadataRoot),
		releases:     make(map[string][]string),
	}
}

// Register associates releaseHash with the ordered virtual paths of its metas.
// Paths are stored without the .meta extension, e.g. "ShowName/episode.s01e01".
func (rs *ReleaseStore) Register(releaseHash string, virtualPaths []string) {
	paths := make([]string, len(virtualPaths))
	copy(paths, virtualPaths)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.releases[releaseHash] = paths
}

// Paths returns the ordered virtual paths registered for releaseHash.
func (rs *ReleaseStore) Paths(releaseHash string) ([]string, bool) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	paths, ok := rs.releases[releaseHash]
	if !ok {
		return nil, false
	}
	out := make([]string, len(paths))
	copy(out, paths)
	return out, true
}

// ReadMeta returns the raw on-disk v3 .meta bytes for the index-th virtual path
// registered under releaseHash.
func (rs *ReleaseStore) ReadMeta(releaseHash string, index int) ([]byte, error) {
	rs.mu.RLock()
	paths, ok := rs.releases[releaseHash]
	rs.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("sharenet: release %s not registered", releaseHash)
	}
	if index < 0 || index >= len(paths) {
		return nil, fmt.Errorf("sharenet: meta index %d out of range (%d metas) for %s", index, len(paths), releaseHash)
	}

	path := filepath.Join(rs.metadataRoot, paths[index]+".meta")

	// Guard against path traversal from a malformed virtualPath stored at register time.
	if !strings.HasPrefix(filepath.Clean(path), rs.metadataRoot) {
		return nil, fmt.Errorf("sharenet: invalid path for release %s", releaseHash)
	}

	return os.ReadFile(path)
}
