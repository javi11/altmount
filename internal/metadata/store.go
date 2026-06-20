package metadata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	lru "github.com/hashicorp/golang-lru/v2"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/nzb"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

const defaultStoreCacheSize = 256

// StoreService reads/writes per-release NzbStore files (zstd proto) and caches
// decompressed stores keyed by store ref (path).
type StoreService struct {
	rootPath string
	cache    *lru.Cache[string, *metapb.NzbStore]
	encoder  *zstd.Encoder
	decoder  *zstd.Decoder
}

// NewStoreService creates a StoreService rooted at rootPath with an LRU cache.
func NewStoreService(rootPath string) *StoreService {
	c, _ := lru.New[string, *metapb.NzbStore](defaultStoreCacheSize)
	enc, _ := zstd.NewWriter(nil)
	dec, _ := zstd.NewReader(nil)
	return &StoreService{rootPath: rootPath, cache: c, encoder: enc, decoder: dec}
}

// WriteStore writes zstd(proto) to ref atomically and refreshes the cache.
func (ss *StoreService) WriteStore(ref string, store *metapb.NzbStore) error {
	raw, err := proto.Marshal(store)
	if err != nil {
		return fmt.Errorf("marshal store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(ref), 0755); err != nil {
		return fmt.Errorf("mkdir store dir: %w", err)
	}
	compressed := ss.encoder.EncodeAll(raw, nil)
	dir := filepath.Dir(ref)
	base := filepath.Base(ref)
	tmpFile, err := os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp store file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, writeErr := tmpFile.Write(compressed); writeErr != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp store file: %w", writeErr)
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp store file: %w", closeErr)
	}
	if err := os.Rename(tmpPath, ref); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename store file: %w", err)
	}
	ss.cache.Add(ref, store)
	return nil
}

// ReadStore reads and decompresses a store, caching the result.
func (ss *StoreService) ReadStore(ref string) (*metapb.NzbStore, error) {
	if c, ok := ss.cache.Get(ref); ok {
		return c, nil
	}
	compressed, err := os.ReadFile(ref)
	if err != nil {
		return nil, fmt.Errorf("read store %q: %w", ref, err)
	}
	raw, err := ss.decoder.DecodeAll(compressed, nil)
	if err != nil {
		return nil, fmt.Errorf("decompress store: %w", err)
	}
	store := &metapb.NzbStore{}
	if err := proto.Unmarshal(raw, store); err != nil {
		return nil, fmt.Errorf("unmarshal store: %w", err)
	}
	ss.cache.Add(ref, store)
	return store, nil
}

// FlatSegments returns all segments in flat order: files in order, each file's
// segments in the order they appear (sorted by number at import time).
func FlatSegments(store *metapb.NzbStore) []*metapb.NzbSeg {
	var out []*metapb.NzbSeg
	for _, f := range store.Files {
		out = append(out, f.Segments...)
	}
	return out
}

// RegenerateNZB reads the store at storePath and returns NZB XML bytes.
// Returns (nil, nil) if the store does not exist.
func (ss *StoreService) RegenerateNZB(storePath string) ([]byte, error) {
	store, err := ss.ReadStore(storePath)
	if err != nil {
		// Unwrap to check for os.ErrNotExist buried inside fmt.Errorf wraps.
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read nzb store: %w", err)
	}
	return nzb.BuildNZB(store), nil
}

// resolveRefs maps SegmentRefs to fully-populated SegmentData using the flat
// segment index. Returns an error if any ref index is out of range.
func resolveRefs(flat []*metapb.NzbSeg, refs []*metapb.SegmentRef) ([]*metapb.SegmentData, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]*metapb.SegmentData, len(refs))
	for i, r := range refs {
		if r.StoreIndex < 0 || int(r.StoreIndex) >= len(flat) {
			return nil, fmt.Errorf("segment ref index %d out of range (%d segments)", r.StoreIndex, len(flat))
		}
		seg := flat[r.StoreIndex]
		out[i] = &metapb.SegmentData{
			Id:          seg.Id,
			SegmentSize: seg.Bytes,
			StartOffset: r.StartOffset,
			EndOffset:   r.EndOffset,
		}
	}
	return out, nil
}
