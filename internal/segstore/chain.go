package segstore

import "github.com/javi11/altmount/internal/usenet"

// chain is a read-through usenet.SegmentStore over an ordered list of backing
// stores. Reads return the first hit; tee-writes (the reader caching a normal
// network fetch) go only to the designated write store.
type chain struct {
	write usenet.SegmentStore   // tee-write target (the large on-disk cache); may be nil
	read  []usenet.SegmentStore // consulted in order, first hit wins
}

// NewChain builds the SegmentStore the reader consults. Reads check cache then
// repair (first hit wins), so reconstructed segments in the repair store are
// served transparently. Tee-writes from normal fetches go only to cache, never
// to the small repair store — that store is reserved for segments written
// directly by the PAR2 sink, so ordinary streaming can't evict recovered data.
//
// nil stores are skipped. When both are nil NewChain returns nil, preserving the
// reader's existing "no store" fast path.
func NewChain(cache, repair usenet.SegmentStore) usenet.SegmentStore {
	var read []usenet.SegmentStore
	if cache != nil {
		read = append(read, cache)
	}
	if repair != nil {
		read = append(read, repair)
	}
	if len(read) == 0 {
		return nil
	}
	return &chain{write: cache, read: read}
}

// Get returns the first hit across the read stores, in order. Invariant: the
// cache is consulted before the repair store, which is safe only because
// missing/corrupt segments are never written to the cache — so a cache hit can
// never shadow a reconstructed segment in the repair store. If that invariant
// ever changes (e.g. the cache starts holding partial/corrupt entries), this
// ordering must be revisited or the cache entry invalidated on repair.
func (c *chain) Get(id string) ([]byte, bool) {
	for _, s := range c.read {
		if b, ok := s.Get(id); ok {
			return b, true
		}
	}
	return nil, false
}

func (c *chain) Put(id string, data []byte) error {
	if c.write == nil {
		return nil
	}
	return c.write.Put(id, data)
}
