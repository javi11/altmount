package nzbfilesystem

import (
	"github.com/javi11/altmount/internal/nzbfilesystem/segcache"
	"github.com/javi11/altmount/internal/usenet"
)

// benchReplayStore is the segment store the replay scenario opens files
// with: the default memory tier, no disk.
func benchReplayStore() usenet.SegmentStore {
	return segcache.NewTieredStore(segcache.NewMemoryCache(256 << 20))
}
