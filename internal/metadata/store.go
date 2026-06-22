package metadata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
		size := seg.Bytes
		if r.DecodedBytes != 0 {
			size = r.DecodedBytes
		}
		out[i] = &metapb.SegmentData{
			Id:          seg.Id,
			SegmentSize: size,
			StartOffset: r.StartOffset,
			EndOffset:   r.EndOffset,
		}
	}
	return out, nil
}

// resolveRuns expands SegmentRuns to fully-populated SegmentData using the flat
// segment index. Each run covers a consecutive range of full-use segments
// (start_offset=0, end_offset=size-1). Produces output byte-identical to the
// explicit-ref path. Returns an error if any run index is out of range.
func resolveRuns(flat []*metapb.NzbSeg, runs []*metapb.SegmentRun) ([]*metapb.SegmentData, error) {
	if len(runs) == 0 {
		return nil, nil
	}
	var total int64
	for _, run := range runs {
		total += run.Count
	}
	out := make([]*metapb.SegmentData, 0, total)
	for _, run := range runs {
		for j := int64(0); j < run.Count; j++ {
			idx := run.BaseStoreIndex + j
			if idx < 0 || int(idx) >= len(flat) {
				return nil, fmt.Errorf("segment run index %d out of range (%d segments)", idx, len(flat))
			}
			seg := flat[idx]
			size := seg.Bytes
			if run.DecodedBytes != 0 {
				size = run.DecodedBytes
			}
			out = append(out, &metapb.SegmentData{
				Id:          seg.Id,
				SegmentSize: size,
				StartOffset: 0,
				EndOffset:   size - 1,
			})
		}
	}
	return out, nil
}

// segDataToRefs converts a slice of SegmentData to SegmentRefs using a flat segment index
// (message-id → position in NzbStore flat segment array). StartOffset and EndOffset are
// preserved from the original SegmentData (archive slicing may have narrowed them). Returns
// nil for a nil or empty input. Returns an error if any segment ID is not present in index.
func segDataToRefs(segments []*metapb.SegmentData, index map[string]int64) ([]*metapb.SegmentRef, error) {
	if len(segments) == 0 {
		return nil, nil
	}
	refs := make([]*metapb.SegmentRef, len(segments))
	for i, seg := range segments {
		idx, ok := index[seg.Id]
		if !ok {
			return nil, fmt.Errorf("segment %q not found in store index", seg.Id)
		}
		refs[i] = &metapb.SegmentRef{
			StoreIndex:   idx,
			StartOffset:  seg.StartOffset,
			EndOffset:    seg.EndOffset,
			DecodedBytes: seg.SegmentSize,
		}
	}
	return refs, nil
}

// isFullUse reports whether a ref uses its whole segment (no archive slicing):
// start at 0 and end at the last decoded byte. Only full-use refs can be folded
// into a SegmentRun, which implies start_offset=0 / end_offset=decoded-1.
func isFullUse(r *metapb.SegmentRef) bool {
	return r.StartOffset == 0 && r.DecodedBytes != 0 && r.EndOffset == r.DecodedBytes-1
}

// splitRefs partitions refs into compact SegmentRuns (maximal stretches of
// consecutive store indices that are full-use and share a decoded size) plus the
// leftover explicit SegmentRefs for anything that can't be folded (partial
// segments at archive/volume seams, or non-consecutive indices). For a plain
// single file the whole array collapses to runs with no leftovers; for an archive
// release the uniform body becomes a handful of runs and only the partial seam
// segments stay explicit.
//
// Runs and refs are reassembled at read time by store index, so this is only safe
// when the refs are strictly increasing by store index. When they are not, the
// original order would be lost on read, so the whole array is kept as explicit
// refs (which preserve order) and no runs are emitted.
func splitRefs(refs []*metapb.SegmentRef) (runs []*metapb.SegmentRun, leftover []*metapb.SegmentRef) {
	if len(refs) == 0 {
		return nil, nil
	}
	for i := 1; i < len(refs); i++ {
		if refs[i].StoreIndex <= refs[i-1].StoreIndex {
			return nil, refs // not strictly increasing — keep explicit, preserve order
		}
	}

	for i := 0; i < len(refs); {
		r := refs[i]
		if !isFullUse(r) {
			leftover = append(leftover, r)
			i++
			continue
		}
		// Extend a run across consecutive, same-size, full-use refs.
		j := i + 1
		for j < len(refs) {
			n := refs[j]
			if n.StoreIndex != refs[j-1].StoreIndex+1 || n.DecodedBytes != r.DecodedBytes || !isFullUse(n) {
				break
			}
			j++
		}
		if j-i >= 2 {
			runs = append(runs, &metapb.SegmentRun{
				BaseStoreIndex: r.StoreIndex,
				Count:          int64(j - i),
				DecodedBytes:   r.DecodedBytes,
			})
		} else {
			// A lone full-use segment is no smaller as a run than as a ref; keep it explicit.
			leftover = append(leftover, r)
		}
		i = j
	}
	return runs, leftover
}

// resolveSegments reconstructs the full SegmentData slice from any combination of
// SegmentRuns and SegmentRefs. Pure-runs and pure-refs inputs preserve their
// stored order directly. A mixed input is merged by store index — safe because
// splitRefs only produces a mix when the segments are strictly increasing by store
// index, so store-index order equals the original segment order.
func resolveSegments(flat []*metapb.NzbSeg, runs []*metapb.SegmentRun, refs []*metapb.SegmentRef) ([]*metapb.SegmentData, error) {
	if len(runs) == 0 {
		return resolveRefs(flat, refs)
	}
	if len(refs) == 0 {
		return resolveRuns(flat, runs)
	}

	type entry struct {
		idx int64
		sd  *metapb.SegmentData
	}
	entries := make([]entry, 0, len(refs)+len(runs))
	for _, r := range refs {
		if r.StoreIndex < 0 || int(r.StoreIndex) >= len(flat) {
			return nil, fmt.Errorf("segment ref index %d out of range (%d segments)", r.StoreIndex, len(flat))
		}
		seg := flat[r.StoreIndex]
		size := seg.Bytes
		if r.DecodedBytes != 0 {
			size = r.DecodedBytes
		}
		entries = append(entries, entry{idx: r.StoreIndex, sd: &metapb.SegmentData{
			Id: seg.Id, SegmentSize: size, StartOffset: r.StartOffset, EndOffset: r.EndOffset,
		}})
	}
	for _, run := range runs {
		for j := int64(0); j < run.Count; j++ {
			idx := run.BaseStoreIndex + j
			if idx < 0 || int(idx) >= len(flat) {
				return nil, fmt.Errorf("segment run index %d out of range (%d segments)", idx, len(flat))
			}
			seg := flat[idx]
			size := seg.Bytes
			if run.DecodedBytes != 0 {
				size = run.DecodedBytes
			}
			entries = append(entries, entry{idx: idx, sd: &metapb.SegmentData{
				Id: seg.Id, SegmentSize: size, StartOffset: 0, EndOffset: size - 1,
			}})
		}
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].idx < entries[b].idx })
	out := make([]*metapb.SegmentData, len(entries))
	for i, e := range entries {
		out[i] = e.sd
	}
	return out, nil
}
