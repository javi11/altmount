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
