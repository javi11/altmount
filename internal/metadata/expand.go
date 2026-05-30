package metadata

import (
	"fmt"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// ExpandSharedOuterSources resolves NestedSegmentSource.SharedOuterSourceIndex
// references in-place. Sources with a non-zero index inherit Segments, AesKey,
// AesIv, and (if unset) InnerVolumeSize from
// meta.SharedOuterSources[index-1]. Slice headers share their underlying
// array — RAM cost is unchanged from the legacy layout. Safe to call on any
// FileMetadata; a no-op when SharedOuterSources is empty.
//
// The dedupe written by archive.NewFileMetadataFromContent is the
// write-side counterpart of this expansion.
func ExpandSharedOuterSources(meta *metapb.FileMetadata) error {
	if len(meta.SharedOuterSources) == 0 {
		return nil
	}
	for _, ns := range meta.NestedSources {
		if ns.SharedOuterSourceIndex == 0 {
			continue
		}
		idx := int(ns.SharedOuterSourceIndex) - 1
		if idx < 0 || idx >= len(meta.SharedOuterSources) {
			return fmt.Errorf(
				"metadata: nested source references shared_outer_source_index %d but only %d shared outer source(s) are defined",
				ns.SharedOuterSourceIndex, len(meta.SharedOuterSources),
			)
		}
		shared := meta.SharedOuterSources[idx]
		ns.Segments = shared.Segments
		ns.AesKey = shared.AesKey
		ns.AesIv = shared.AesIv
		if ns.InnerVolumeSize == 0 {
			ns.InnerVolumeSize = shared.InnerVolumeSize
		}
	}
	return nil
}
