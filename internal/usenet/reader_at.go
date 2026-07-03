package usenet

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/javi11/altmount/internal/pool"
)

// segmentsReaderAt adapts a SegmentLoader-backed logical file to io.ReaderAt.
// It is designed for sparse header reads (container probing): each ReadAt
// builds a bounded segment range and a short-lived UsenetReader; a small
// per-instance SegmentStore caches decoded articles so repeated header hops
// into the same segment fetch it from the network only once.
type segmentsReaderAt struct {
	ctx        context.Context
	loader     SegmentLoader
	poolGetter func() (pool.NntpClient, error)
	metrics    MetricsTracker
	size       int64
	cache      *boundedSegmentStore
}

// NewSegmentsReaderAt returns an io.ReaderAt over the logical file described
// by loader. size is the logical file size in bytes. The reader is safe for
// sequential use by a single prober; it is not optimized for large reads.
func NewSegmentsReaderAt(
	ctx context.Context,
	loader SegmentLoader,
	poolGetter func() (pool.NntpClient, error),
	metrics MetricsTracker,
	size int64,
) io.ReaderAt {
	return &segmentsReaderAt{
		ctx:        ctx,
		loader:     loader,
		poolGetter: poolGetter,
		metrics:    metrics,
		size:       size,
		cache:      newBoundedSegmentStore(8),
	}
}

func (r *segmentsReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("segments reader: negative offset %d", off)
	}
	if off >= r.size {
		return 0, io.EOF
	}
	want := int64(len(p))
	if off+want > r.size {
		want = r.size - off
	}
	if want == 0 {
		return 0, nil
	}

	rg := GetSegmentsInRange(r.ctx, off, off+want-1, r.loader)
	ur, err := NewUsenetReader(r.ctx, r.poolGetter, rg, 1, r.metrics, "", r.cache)
	if err != nil {
		return 0, fmt.Errorf("segments reader: %w", err)
	}
	defer func() { _ = ur.Close() }()

	n, err := io.ReadFull(ur, p[:want])
	if err != nil {
		return n, err
	}
	if want < int64(len(p)) {
		return n, io.EOF
	}
	return n, nil
}

// boundedSegmentStore is a SegmentStore with FIFO eviction, sized for the
// handful of segments a container probe touches.
type boundedSegmentStore struct {
	mu       sync.Mutex
	capacity int
	order    []string
	entries  map[string][]byte
}

func newBoundedSegmentStore(capacity int) *boundedSegmentStore {
	return &boundedSegmentStore{
		capacity: capacity,
		entries:  make(map[string][]byte, capacity),
	}
}

func (s *boundedSegmentStore) Get(messageID string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.entries[messageID]
	return data, ok
}

func (s *boundedSegmentStore) Put(messageID string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.entries[messageID]; exists {
		return nil
	}
	for len(s.order) >= s.capacity {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.entries, oldest)
	}
	s.entries[messageID] = data
	s.order = append(s.order, messageID)
	return nil
}
