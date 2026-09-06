package usenet

import (
	"context"
	"errors"
	"io"
	"sync"
)

type Segment struct {
	Id    string
	Start int64
	End   int64 // End offset in the segment (inclusive)
	Size  int64 // Size of the segment in bytes
}

var (
	ErrBufferNotReady = errors.New("buffer not ready")
	ErrSegmentLimit   = errors.New("segment limit reached")
)

type segmentRange struct {
	start    int64
	end      int64
	segments []*segment
	current  int
	ctx      context.Context
	mu       sync.RWMutex

	// Lazy creation support (nil loader = eager/pre-populated mode)
	loader       SegmentLoader
	startSegIdx  int   // Loader index of first segment in range
	startFilePos int64 // File offset at start of first segment's usable data
	endFilePos   int64 // File offset at start of last segment's usable data
}

func (r *segmentRange) HasSegments() bool {
	return len(r.segments) > 0
}

// GetCurrentIndex returns the current segment index being read
func (r *segmentRange) GetCurrentIndex() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

func (r *segmentRange) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.segments)
}

func (r *segmentRange) Get() (*segment, error) {
	r.mu.RLock()
	idx := r.current
	r.mu.RUnlock()

	return r.GetSegment(idx)
}

func (r *segmentRange) GetSegment(index int) (*segment, error) {
	r.mu.RLock()
	if index < 0 || index >= len(r.segments) {
		r.mu.RUnlock()
		return nil, ErrSegmentLimit
	}
	seg := r.segments[index]
	r.mu.RUnlock()

	if seg != nil {
		return seg, nil
	}

	// No loader means eager mode — a nil slot is a real nil.
	if r.loader == nil {
		return nil, ErrSegmentLimit
	}

	// Upgrade to write lock for lazy creation.
	r.mu.Lock()
	defer r.mu.Unlock()

	// Re-check bounds: a concurrent Clear() may have nil-ed the slice
	// between releasing the read lock and acquiring the write lock.
	if index >= len(r.segments) {
		return nil, ErrSegmentLimit
	}

	// Double-check after acquiring write lock.
	if r.segments[index] != nil {
		return r.segments[index], nil
	}

	seg = r.createSegmentLocked(index)
	if seg == nil {
		return nil, ErrSegmentLimit
	}
	r.segments[index] = seg
	return seg, nil
}

// createSegmentLocked creates a segment on demand for the given range-local index.
// Caller must hold r.mu in write mode. Returns nil if the loader has no segment at this index.
func (r *segmentRange) createSegmentLocked(index int) *segment {
	loaderIdx := r.startSegIdx + index
	src, groups, ok := r.loader.GetSegment(loaderIdx)
	if !ok {
		return nil
	}

	usableLen := src.End - src.Start + 1
	if usableLen <= 0 {
		return nil
	}

	readStart := src.Start
	readEnd := src.End

	totalSegs := len(r.segments)

	// Trim first segment if request starts partway through it
	if index == 0 && r.start > r.startFilePos {
		delta := r.start - r.startFilePos
		readStart = src.Start + delta
	}

	// Trim last segment if request ends before the segment's usable data ends
	if index == totalSegs-1 {
		segFileEnd := r.endFilePos + usableLen - 1
		if r.end < segFileEnd {
			delta := segFileEnd - r.end
			readEnd = src.End - delta
		}
	}

	if readStart > readEnd {
		return nil
	}

	return newSegment(src.Id, readStart, readEnd, src.Size, groups, loaderIdx)
}

func (r *segmentRange) Next() (*segment, error) {
	r.mu.Lock()
	if r.current >= len(r.segments) {
		r.mu.Unlock()
		return nil, ErrSegmentLimit
	}

	// Release data from consumed segment to allow GC
	r.segments[r.current].Release()
	r.segments[r.current] = nil

	r.current += 1
	r.mu.Unlock()

	return r.Get()
}

func (r *segmentRange) CloseWithError(err error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.segments {
		if s != nil {
			s.SetError(err)
		}
	}
}

func (r *segmentRange) CloseSegments() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.segments {
		if s != nil {
			s.Release()
		}
	}
}

func (r *segmentRange) Clear() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, s := range r.segments {
		if s != nil {
			s.Release()
		}
	}

	r.segments = nil

	return nil
}

type segment struct {
	Id          string
	Start       int64
	End         int64
	SegmentSize int64
	groups      []string
	// loaderIdx is the segment's index in the loader's (file's) segment
	// space, independent of the range-local position. Hole bookkeeping is
	// keyed on it so persisted hole maps line up across reads.
	loaderIdx int

	// art holds the bytes this segment serves. It starts private and is
	// swapped for the shared flight-map buffer when a fetch begins, so
	// readers of the same article share one download. SetData/SetError
	// detach back to a private buffer: a zero-filled hole or a patch is one
	// reader's policy, not the article's content.
	art      *articleBuf
	shared   bool
	fm       *flightMap // map the shared article was acquired from
	reader   *segmentReader
	notify   chan struct{} // closed and replaced when art is swapped or the segment released
	mx       sync.Mutex
	released bool
}

// newSegment creates a segment with a private, empty article buffer.
// loaderIdx is the segment's index in the loader's segment space.
func newSegment(id string, start, end, segmentSize int64, groups []string, loaderIdx int) *segment {
	return &segment{
		Id:          id,
		Start:       start,
		End:         end,
		SegmentSize: segmentSize,
		groups:      groups,
		loaderIdx:   loaderIdx,
		art:         newArticleBuf(segmentSize),
		notify:      make(chan struct{}),
	}
}

// wakeLocked releases readers parked on this segment. Caller holds mx.
func (s *segment) wakeLocked() {
	close(s.notify)
	s.notify = make(chan struct{})
}

// attachShared points the segment at the article's shared buffer so a fetch
// (by this reader or another) serves every segment wanting that article.
// Returns the buffer to fetch into.
func (s *segment) attachShared(fm *flightMap) *articleBuf {
	s.mx.Lock()
	defer s.mx.Unlock()
	if s.shared || s.released {
		return s.art
	}
	a := fm.acquire(s.Id, s.SegmentSize)
	s.art = a
	s.shared = true
	s.fm = fm
	s.wakeLocked()
	return a
}

// detachLocked swaps a shared buffer for a fresh private one. Caller holds mx.
func (s *segment) detachLocked() {
	if !s.shared {
		return
	}
	s.fm.release(s.Id, s.art)
	s.art = newArticleBuf(s.SegmentSize)
	s.shared = false
	s.wakeLocked()
}

// attemptWriter, finish and published operate on whatever buffer the segment
// currently serves; the fetch path uses the shared buffer directly.
func (s *segment) attemptWriter() *articleWriter {
	s.mx.Lock()
	a := s.art
	s.mx.Unlock()
	return a.attemptWriter()
}

func (s *segment) finish(w *articleWriter) { w.a.finish(w) }

func (s *segment) published() int64 {
	s.mx.Lock()
	a := s.art
	s.mx.Unlock()
	return a.published()
}

// SetData stores a complete payload for this segment only.
// Non-blocking, safe to call from any goroutine.
func (s *segment) SetData(data []byte) {
	if s == nil {
		return
	}
	s.mx.Lock()
	if s.released {
		s.mx.Unlock()
		return
	}
	s.detachLocked()
	a := s.art
	s.mx.Unlock()
	a.setData(data)
}

// SetError records a download error for this segment only. Readers already
// handed a prefix see the error once they reach the watermark.
func (s *segment) SetError(err error) {
	if s == nil || err == nil {
		return
	}
	s.mx.Lock()
	if s.released {
		s.mx.Unlock()
		return
	}
	if s.shared {
		// Keep the bytes the shared article already delivered: swap in a
		// private copy that ends in the error.
		old := s.art
		s.detachLocked()
		old.mu.Lock()
		s.art.buf, s.art.ready = old.buf, old.ready
		old.mu.Unlock()
	}
	a := s.art
	s.mx.Unlock()
	a.setError(err)
}

// GetDownloadError returns any download error recorded for this segment.
func (s *segment) GetDownloadError() error {
	if s == nil {
		return nil
	}
	s.mx.Lock()
	a := s.art
	released := s.released
	s.mx.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	if released {
		return io.ErrClosedPipe
	}
	return nil
}

// DataLen returns how many decoded bytes are currently available.
func (s *segment) DataLen() int {
	if s == nil {
		return 0
	}
	return int(s.published())
}

// segmentReader serves [Start, End] of the segment, waiting only for the
// bytes each Read needs rather than for the whole article.
type segmentReader struct {
	s   *segment
	ctx context.Context
	off int64 // absolute offset into the article
}

// GetReaderContext returns the segment's reader. The context bounds waits
// for bytes that have not arrived yet; a later call may supply a fresh one.
func (s *segment) GetReaderContext(ctx context.Context) io.Reader {
	s.mx.Lock()
	defer s.mx.Unlock()
	if s.reader == nil {
		s.reader = &segmentReader{s: s, off: s.Start}
	}
	s.reader.ctx = ctx
	return s.reader
}

// GetReader returns a reader that blocks without a cancellation bound.
// Prefer GetReaderContext.
func (s *segment) GetReader() io.Reader {
	return s.GetReaderContext(context.Background())
}

func (r *segmentReader) Read(p []byte) (int, error) {
	s := r.s
	if r.off > s.End {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	for {
		s.mx.Lock()
		if s.released {
			s.mx.Unlock()
			return 0, io.ErrClosedPipe
		}
		a := s.art
		shared := s.shared
		segWait := s.notify
		ctx := r.ctx
		s.mx.Unlock()

		a.mu.Lock()
		if a.err != nil && a.ready <= r.off && !shared {
			err := a.err
			a.mu.Unlock()
			return 0, err
		}
		// A shared article's failure is not this reader's verdict: its owner
		// decides to pad or fail and detaches either way, which wakes segWait.
		if a.ready > r.off {
			end := min(a.ready, s.End+1)
			n := copy(p, a.buf[r.off:end])
			r.off += int64(n)
			a.mu.Unlock()
			return n, nil
		}
		if a.done && a.err == nil {
			// A complete article shorter than the requested range ends the
			// segment, matching the previous LimitReader behaviour.
			a.mu.Unlock()
			return 0, io.EOF
		}
		artWait := a.notify
		a.mu.Unlock()
		if ctx == nil {
			ctx = context.Background()
		}
		select {
		case <-artWait:
		case <-segWait:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

// Release drops this segment's hold on its bytes. Safe to call repeatedly.
// A shared article stays alive for other readers still using it.
func (s *segment) Release() {
	if s == nil {
		return
	}
	s.mx.Lock()
	if s.released {
		s.mx.Unlock()
		return
	}
	s.released = true
	if s.shared {
		s.fm.release(s.Id, s.art)
		s.shared = false
	}
	s.art = newArticleBuf(0)
	s.wakeLocked()
	s.mx.Unlock()
}

// Close releases the segment data. Kept for API compatibility with segmentRange.
func (s *segment) Close() error {
	s.Release()
	return nil
}

// CloseWithError stores the error and releases the segment.
func (s *segment) CloseWithError(err error) error {
	if s == nil {
		return nil
	}
	s.SetError(err)
	return nil
}

func (s *segment) ID() string {
	return s.Id
}

func (s *segment) Groups() []string {
	return s.groups
}
