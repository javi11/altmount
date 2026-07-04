package metadata

import (
	"github.com/javi11/altmount/internal/holes"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// AddKnownHoles merges newly confirmed missing-segment runs into a file's
// persisted hole map, using the same read-modify-write path as status
// updates. Merging goes through holes.Accumulator, so the write is idempotent
// and concurrent discoveries collapse into maximal runs.
func (ms *MetadataService) AddKnownHoles(virtualPath string, runs []holes.Run) error {
	if len(runs) == 0 {
		return nil
	}
	return ms.UpdateFileMetadata(virtualPath, func(metadata *metapb.FileMetadata) {
		var acc holes.Accumulator
		acc.Load(KnownHolesFromProto(metadata.KnownHoles))
		acc.Load(runs)
		metadata.KnownHoles = KnownHolesToProto(acc.Runs())
	})
}

// KnownHolesToProto converts accumulator runs for storage.
func KnownHolesToProto(runs []holes.Run) []*metapb.HoleRun {
	if len(runs) == 0 {
		return nil
	}
	out := make([]*metapb.HoleRun, 0, len(runs))
	for _, r := range runs {
		out = append(out, &metapb.HoleRun{StartSegment: int64(r.Start), Count: int64(r.Count)})
	}
	return out
}

// KnownHolesFromProto rebuilds runs from storage; malformed rows are dropped.
func KnownHolesFromProto(rows []*metapb.HoleRun) []holes.Run {
	out := make([]holes.Run, 0, len(rows))
	for _, r := range rows {
		if r == nil || r.StartSegment < 0 || r.Count <= 0 {
			continue
		}
		out = append(out, holes.Run{Start: int(r.StartSegment), Count: int(r.Count)})
	}
	return out
}
