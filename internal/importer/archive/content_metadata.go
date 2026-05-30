package archive

import (
	"time"
	"unsafe"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// NewFileMetadataFromContent creates a FileMetadata from a Content (with its NestedSources)
// for the metadata system. It mirrors the conversion previously inlined inside
// rar.CreateFileMetadataFromRarContent and sevenzip.CreateFileMetadataFromSevenZipContent
// so that non-RAR/non-7z callers (e.g. ISO expansion) can produce equivalent metadata.
//
// Behaviour:
//   - Sets CreatedAt/ModifiedAt to time.Now().Unix().
//   - Defaults Status to FILE_STATUS_HEALTHY.
//   - Copies SegmentData from content.Segments.
//   - When content.AesKey is non-empty, sets Encryption=AES with key/iv.
//   - Appends one NestedSegmentSource per content.NestedSources entry.
func NewFileMetadataFromContent(
	content Content,
	sourceNzbPath string,
	releaseDate int64,
	nzbdavId string,
) *metapb.FileMetadata {
	now := time.Now().Unix()

	meta := &metapb.FileMetadata{
		FileSize:      content.Size,
		SourceNzbPath: sourceNzbPath,
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		CreatedAt:     now,
		ModifiedAt:    now,
		SegmentData:   content.Segments,
		ReleaseDate:   releaseDate,
		NzbdavId:      nzbdavId,
	}

	// Set AES encryption if keys are present (single-layer encrypted archive)
	if len(content.AesKey) > 0 {
		meta.Encryption = metapb.Encryption_AES
		meta.AesKey = content.AesKey
		meta.AesIv = content.AesIV
	}

	// Carry the per-clip timeline table for multi-clip BD main features.
	// Empty for everything else, which keeps the read-path remux filter
	// disabled for all other files.
	for _, cb := range content.ClipBoundaries {
		meta.ClipBoundaries = append(meta.ClipBoundaries, &metapb.ClipBoundary{
			ByteLen:   cb.ByteLen,
			Delta_90K: cb.Delta90k,
		})
	}

	// Populate nested sources. For multi-extent encrypted volumes (e.g. a
	// Blu-ray main feature with hundreds of extents that all read from the
	// same encrypted RAR) every NestedSource shares the same Segments slice
	// in memory. Serialising them naïvely duplicates the segment list per
	// extent — for Avatar 3D that produced an 8 GB .meta file. We dedupe
	// here by detecting shared segment-list backing arrays and emitting
	// one entry in meta.SharedOuterSources per unique group; each
	// NestedSource then carries only its inner_offset + inner_length plus
	// a 1-based shared_outer_source_index. Sources without sharing fall
	// through to the legacy on-disk layout so old code paths are unaffected.
	appendNestedSourcesWithDedupe(meta, content.NestedSources)

	return meta
}

// nestedSourceShareKey identifies a NestedSource by the backing array of its
// Segments slice plus the AES key/IV and inner volume size. Sources with the
// same key can share one entry in FileMetadata.SharedOuterSources.
type nestedSourceShareKey struct {
	segmentsPtr     uintptr
	segmentsLen     int
	aesKey          string
	aesIv           string
	innerVolumeSize int64
}

// shareKeyFor builds a sharing key. It uses the backing-array pointer of
// the Segments slice (cheap O(1) check) plus the slice length to catch
// accidental pointer reuse across distinct slices. The AES key/iv and
// inner_volume_size complete the identity — two sources are only
// shareable when those match exactly.
func shareKeyFor(ns NestedSource) nestedSourceShareKey {
	var ptr uintptr
	if len(ns.Segments) > 0 {
		ptr = uintptr(unsafe.Pointer(unsafe.SliceData(ns.Segments)))
	}
	return nestedSourceShareKey{
		segmentsPtr:     ptr,
		segmentsLen:     len(ns.Segments),
		aesKey:          string(ns.AesKey),
		aesIv:           string(ns.AesIV),
		innerVolumeSize: ns.InnerVolumeSize,
	}
}

// appendNestedSourcesWithDedupe writes the NestedSources into meta,
// deduplicating shared outer-segment data into meta.SharedOuterSources.
// When fewer than two sources qualify for sharing (e.g. a single source,
// or every source has a unique segment list) the legacy layout is used:
// every NestedSegmentSource carries its own Segments + AesKey + AesIv.
func appendNestedSourcesWithDedupe(meta *metapb.FileMetadata, sources []NestedSource) {
	if len(sources) == 0 {
		return
	}

	// First pass: count how many sources share each key. Only keys that
	// appear in >= 2 sources are worth deduping (single-use keys cost more
	// to store as shared entries than as inline data).
	counts := make(map[nestedSourceShareKey]int, len(sources))
	for _, ns := range sources {
		if len(ns.Segments) == 0 {
			continue
		}
		counts[shareKeyFor(ns)]++
	}

	// Build the SharedOuterSources slice, preserving first-appearance order.
	keyToIndex := make(map[nestedSourceShareKey]int32, len(counts))
	for _, ns := range sources {
		if len(ns.Segments) == 0 {
			continue
		}
		key := shareKeyFor(ns)
		if counts[key] < 2 {
			continue
		}
		if _, seen := keyToIndex[key]; seen {
			continue
		}
		meta.SharedOuterSources = append(meta.SharedOuterSources, &metapb.NestedSegmentSource{
			Segments:        ns.Segments,
			AesKey:          ns.AesKey,
			AesIv:           ns.AesIV,
			InnerVolumeSize: ns.InnerVolumeSize,
		})
		keyToIndex[key] = int32(len(meta.SharedOuterSources)) // 1-based
	}

	// Second pass: emit one NestedSegmentSource per input, referencing
	// the shared entry where applicable.
	for _, ns := range sources {
		entry := &metapb.NestedSegmentSource{
			InnerOffset: ns.InnerOffset,
			InnerLength: ns.InnerLength,
		}
		if idx, ok := keyToIndex[shareKeyFor(ns)]; ok && len(ns.Segments) > 0 {
			entry.SharedOuterSourceIndex = idx
		} else {
			entry.Segments = ns.Segments
			entry.AesKey = ns.AesKey
			entry.AesIv = ns.AesIV
			entry.InnerVolumeSize = ns.InnerVolumeSize
		}
		meta.NestedSources = append(meta.NestedSources, entry)
	}
}

// The read-side counterpart of the dedupe written here lives in
// internal/metadata.ExpandSharedOuterSources — called from
// MetadataService.ReadFileMetadata after proto.Unmarshal.
