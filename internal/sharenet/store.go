package sharenet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ReleaseStore maps releaseHash → virtualPath and reads the on-disk
// .meta and .seg files that the metaserver needs to serve.
// virtualPath is relative to metadataRoot, e.g. "ShowName/episode.s01e01"
// which maps to "{metadataRoot}/ShowName/episode.s01e01.meta".
type ReleaseStore struct {
	mu           sync.RWMutex
	metadataRoot string
	releases     map[string]string // releaseHash → virtualPath
}

// NewReleaseStore creates a store rooted at metadataRoot.
// metadataRoot should be the same as config.Metadata.RootPath.
func NewReleaseStore(metadataRoot string) *ReleaseStore {
	return &ReleaseStore{
		metadataRoot: filepath.Clean(metadataRoot),
		releases:     make(map[string]string),
	}
}

// Register associates releaseHash with virtualPath.
// virtualPath is the path written by WriteFileMetadata, without the .meta
// extension, e.g. "ShowName/episode.s01e01".
func (rs *ReleaseStore) Register(releaseHash, virtualPath string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.releases[releaseHash] = virtualPath
}

// Lookup returns the virtualPath for a releaseHash.
func (rs *ReleaseStore) Lookup(releaseHash string) (string, bool) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	vp, ok := rs.releases[releaseHash]
	return vp, ok
}

// ReadMeta returns the raw bytes of the .meta file for releaseHash.
func (rs *ReleaseStore) ReadMeta(releaseHash string) ([]byte, error) {
	return rs.readFile(releaseHash, ".meta")
}

// ReadSeg returns the raw bytes of the .seg sidecar for releaseHash.
func (rs *ReleaseStore) ReadSeg(releaseHash string) ([]byte, error) {
	return rs.readFile(releaseHash, ".seg")
}

func (rs *ReleaseStore) readFile(releaseHash, ext string) ([]byte, error) {
	rs.mu.RLock()
	virtualPath, ok := rs.releases[releaseHash]
	rs.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("sharenet: release %s not registered", releaseHash)
	}

	path := filepath.Join(rs.metadataRoot, virtualPath+ext)

	// Guard against path traversal from a malformed virtualPath stored at register time.
	if !strings.HasPrefix(filepath.Clean(path), rs.metadataRoot) {
		return nil, fmt.Errorf("sharenet: invalid path for release %s", releaseHash)
	}

	return os.ReadFile(path)
}
