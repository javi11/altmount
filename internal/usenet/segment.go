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

	// Progressive handoff. buf holds whatever the current attempt has
	// decoded; ready is how much of it readers may see. A later attempt
	// starts a fresh buf and swaps it in only once it has passed ready, so a
	// reader that already consumed the prefix never sees it move.
	buf       []byte
	ready     int64
	done      bool
	dataErr   error
	attempt   int
	notify    chan struct{} // closed and replaced on every publish
	dataReady chan struct{} // closed once, on completion or error
	readyOnce sync.Once

	reader   *segmentReader
	mx       sync.Mutex
	released bool
}

// newSegment creates a segment with an initialized dataReady channel.
// loaderIdx is the segment's index in the loader's segment space.
func newSegment(id string, start, end, segmentSize int64, groups []string, loaderIdx int) *segment {
	return &segment{
		Id:          id,
		Start:       start,
		End:         end,
		SegmentSize: segmentSize,
		groups:      groups,
		loaderIdx:   loaderIdx,
		notify:      make(chan struct{}),
		dataReady:   make(chan struct{}),
	}
}

// signalReady safely closes the dataReady channel exactly once.
func (s *segment) signalReady() {
	s.readyOnce.Do(func() {
		close(s.dataReady)
	})
}

// wakeLocked releases every reader parked on notify. Caller holds mx.
func (s *segment) wakeLocked() {
	close(s.notify)
	s.notify = make(chan struct{})
}

// segmentWriter is one fetch attempt's sink. Bytes written are published to
// readers as they arrive.
type segmentWriter struct {
	s       *segment
	attempt int
	buf     []byte
}

// attemptWriter starts a new fetch attempt. Only the newest attempt can
// publish, and only past what earlier attempts already made visible.
func (s *segment) attemptWriter() *segmentWriter {
	s.mx.Lock()
	defer s.mx.Unlock()
	s.attempt++
	size := s.SegmentSize
	if size < 0 {
		size = 0
	}
	return &segmentWriter{s: s, attempt: s.attempt, buf: make([]byte, 0, size)}
}

func (w *segmentWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	w.s.publish(w)
	return len(p), nil
}

// bytes returns everything this attempt received.
func (w *segmentWriter) bytes() []byte { return w.buf }

func (s *segment) publish(w *segmentWriter) {
	s.mx.Lock()
	if s.released || s.done || w.attempt != s.attempt || int64(len(w.buf)) <= s.ready {
		s.mx.Unlock()
		return
	}
	s.buf = w.buf
	s.ready = int64(len(w.buf))
	s.wakeLocked()
	s.mx.Unlock()
}

// finish marks the segment complete with the bytes w received.
func (s *segment) finish(w *segmentWriter) {
	s.mx.Lock()
	if s.released || s.done || w.attempt != s.attempt {
		s.mx.Unlock()
		return
	}
	s.buf = w.buf
	s.ready = int64(len(w.buf))
	s.done = true
	s.wakeLocked()
	s.mx.Unlock()
	s.signalReady()
}

// published reports how many bytes readers may currently see.
func (s *segment) published() int64 {
	s.mx.Lock()
	defer s.mx.Unlock()
	return s.ready
}

// SetData stores a complete payload and signals readers.
// Non-blocking, safe to call from any goroutine.
func (s *segment) SetData(data []byte) {
	if s == nil {
		return
	}
	s.mx.Lock()
	if s.released || s.done {
		s.mx.Unlock()
		return
	}
	s.buf = data
	s.ready = int64(len(data))
	s.done = true
	s.wakeLocked()
	s.mx.Unlock()
	s.signalReady()
}

// SetError stores a download error and signals readers. Readers that were
// already handed a prefix see the error once they reach the watermark.
func (s *segment) SetError(err error) {
	if s == nil || err == nil {
		return
	}
	s.mx.Lock()
	if s.dataErr == nil {
		s.dataErr = err
	}
	s.wakeLocked()
	s.mx.Unlock()
	s.signalReady()
}

// GetDownloadError returns any download error that occurred.
func (s *segment) GetDownloadError() error {
	if s == nil {
		return nil
	}
	s.mx.Lock()
	defer s.mx.Unlock()
	return s.dataErr
}

// DataLen returns how many decoded bytes are currently available.
func (s *segment) DataLen() int {
	if s == nil {
		return 0
	}
	s.mx.Lock()
	defer s.mx.Unlock()
	return int(s.ready)
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
		if s.dataErr != nil && (s.released || s.ready <= r.off) {
			err := s.dataErr
			s.mx.Unlock()
			return 0, err
		}
		if s.ready > r.off {
			end := min(s.ready, s.End+1)
			n := copy(p, s.buf[r.off:end])
			r.off += int64(n)
			s.mx.Unlock()
			return n, nil
		}
		if s.done {
			// A complete article shorter than the requested range ends the
			// segment, matching the previous LimitReader behaviour.
			s.mx.Unlock()
			return 0, io.EOF
		}
		wait := s.notify
		ctx := r.ctx
		s.mx.Unlock()
		if ctx == nil {
			ctx = context.Background()
		}
		select {
		case <-wait:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

// Release frees the segment data to allow GC. Safe to call multiple times.
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
	s.buf = nil
	if s.dataErr == nil {
		s.dataErr = io.ErrClosedPipe
	}
	s.wakeLocked()
	s.mx.Unlock()

	s.signalReady()
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
