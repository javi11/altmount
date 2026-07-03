package metadata

import (
	"github.com/javi11/altmount/internal/mediaprobe"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// UpdateMediaStructure persists the probed container layout of a video file
// into its metadata, using the same read-modify-write path as status updates.
func (ms *MetadataService) UpdateMediaStructure(virtualPath string, s *metapb.MediaStructure) error {
	return ms.UpdateFileMetadata(virtualPath, func(metadata *metapb.FileMetadata) {
		metadata.MediaStructure = s
	})
}

// MediaStructureToProto converts a probed mediaprobe.Structure for storage.
// The file size is not stored; it is re-derived from FileMetadata.FileSize.
func MediaStructureToProto(s *mediaprobe.Structure) *metapb.MediaStructure {
	if s == nil {
		return nil
	}
	return &metapb.MediaStructure{
		Container:       s.Container,
		DurationSeconds: s.DurationSeconds,
		Critical:        rangesToProto(s.Critical),
		Payload:         rangesToProto(s.Payload),
		SeekOnly:        rangesToProto(s.SeekOnly),
	}
}

// MediaStructureFromProto rebuilds a mediaprobe.Structure for classification.
// fileSize must be the virtual file's size (FileMetadata.FileSize).
func MediaStructureFromProto(p *metapb.MediaStructure, fileSize int64) *mediaprobe.Structure {
	if p == nil {
		return nil
	}
	return &mediaprobe.Structure{
		Container:       p.Container,
		FileSize:        fileSize,
		DurationSeconds: p.DurationSeconds,
		Critical:        rangesFromProto(p.Critical),
		Payload:         rangesFromProto(p.Payload),
		SeekOnly:        rangesFromProto(p.SeekOnly),
	}
}

func rangesToProto(ranges []mediaprobe.ByteRange) []*metapb.ProbeRange {
	out := make([]*metapb.ProbeRange, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, &metapb.ProbeRange{Start: r.Start, End: r.End, Label: r.Label})
	}
	return out
}

func rangesFromProto(ranges []*metapb.ProbeRange) []mediaprobe.ByteRange {
	out := make([]mediaprobe.ByteRange, 0, len(ranges))
	for _, r := range ranges {
		if r == nil {
			continue
		}
		out = append(out, mediaprobe.ByteRange{Start: r.Start, End: r.End, Label: r.Label})
	}
	return out
}
