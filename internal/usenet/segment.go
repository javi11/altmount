package usenet

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"
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
	defer r.mu.RUnlock()

	if r.current >= len(r.segments) {
		return nil, ErrSegmentLimit
	}

	return r.segments[r.current], nil
}

func (r *segmentRange) GetSegment(index int) (*segment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if index < 0 || index >= len(r.segments) {
		return nil, ErrSegmentLimit
	}

	return r.segments[index], nil
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

	// Data handoff fields (replaces io.Pipe)
	data      []byte       // Downloaded segment data (set once by downloader)
	dataErr   error        // Download error (set once by downloader)
	dataReady chan struct{} // Closed when data or dataErr is set
	readyOnce sync.Once    // Guards closing dataReady channel

	limitedReader io.Reader  // Cached limited reader
	readerReady   bool       // Whether limitedReader has been successfully initialized
	mx            sync.Mutex // Protects released flag, limitedReader, readerReady
	released      bool       // Tracks if segment data has been released
}

// newSegment creates a segment with an initialized dataReady channel.
func newSegment(id string, start, end, segmentSize int64, groups []string) *segment {
	return &segment{
		Id:          id,
		Start:       start,
		End:         end,
		SegmentSize: segmentSize,
		groups:      groups,
		dataReady:   make(chan struct{}),
	}
}

// signalReady safely closes the dataReady channel exactly once.
func (s *segment) signalReady() {
	s.readyOnce.Do(func() {
		close(s.dataReady)
	})
}

// SetData stores the downloaded data and signals readers.
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
	s.data = data
	s.mx.Unlock()

	s.signalReady()
}

// SetError stores a download error and signals readers.
// Non-blocking, safe to call from any goroutine.
func (s *segment) SetError(err error) {
	if s == nil || err == nil {
		return
	}
	s.mx.Lock()
	if s.dataErr == nil {
		s.dataErr = err
	}
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

// DataLen returns the length of the downloaded data.
// Returns 0 if data hasn't been set yet.
func (s *segment) DataLen() int {
	if s == nil {
		return 0
	}
	s.mx.Lock()
	defer s.mx.Unlock()
	return len(s.data)
}

// GetReaderContext returns a reader for the segment data.
// Blocks until data is available, an error is set, or the context is cancelled.
// The reader is limited to the range [Start, End] within the segment.
// If the context is cancelled before data arrives, returns an errorReader.
// Subsequent calls with a valid context will retry if the previous attempt
// was a context cancellation (unlike sync.Once which never retries).
func (s *segment) GetReaderContext(ctx context.Context) io.Reader {
	s.mx.Lock()

	// Fast path: reader already initialized successfully
	if s.readerReady {
		r := s.limitedReader
		s.mx.Unlock()
		return r
	}

	// Check if we already have a non-context error cached
	if s.limitedReader != nil {
		if er, ok := s.limitedReader.(*errorReader); ok {
			// If it was a real error (not context), return it
			if !errors.Is(er.err, context.Canceled) && !errors.Is(er.err, context.DeadlineExceeded) {
				r := s.limitedReader
				s.mx.Unlock()
				return r
			}
			// Previous was a context error — allow retry
			s.limitedReader = nil
		}
	}

	s.mx.Unlock()

	// Wait for data or context cancellation
	waitStart := time.Now()
	select {
	case <-s.dataReady:
		// Data (or error) is ready
		waitDur := time.Since(waitStart)
		if waitDur > 50*time.Millisecond {
			slog.Default().DebugContext(ctx, "reader stalled waiting for segment data",
				"segment_id", s.Id,
				"wait_dur", waitDur,
			)
		}
	case <-ctx.Done():
		return &errorReader{err: ctx.Err()}
	}

	s.mx.Lock()
	defer s.mx.Unlock()

	// Double-check: another goroutine may have initialized while we waited
	if s.readerReady {
		return s.limitedReader
	}

	// Check for download error
	if s.dataErr != nil {
		s.limitedReader = &errorReader{err: s.dataErr}
		s.readerReady = true
		return s.limitedReader
	}

	// Create a reader over the full data
	fullReader := bytes.NewReader(s.data)

	// Skip to Start position
	if s.Start > 0 {
		if _, seekErr := fullReader.Seek(s.Start, io.SeekStart); seekErr != nil {
			if s.dataErr == nil {
				s.dataErr = seekErr
			}
			s.limitedReader = &errorReader{err: seekErr}
			s.readerReady = true
			return s.limitedReader
		}
	}

	// Create LimitReader for the range [Start, End]
	s.limitedReader = io.LimitReader(fullReader, s.End-s.Start+1)
	s.readerReady = true
	return s.limitedReader
}

// GetReader returns a reader for the segment data.
// Blocks indefinitely until data is available or an error is set.
// Prefer GetReaderContext for cancellation support.
func (s *segment) GetReader() io.Reader {
	return s.GetReaderContext(context.Background())
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
	s.data = nil
	if s.dataErr == nil {
		s.dataErr = io.ErrClosedPipe
	}
	s.mx.Unlock()

	// Ensure dataReady is closed so any waiting readers unblock
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

// errorReader is a reader that always returns an error.
type errorReader struct {
	err error
}

func (r *errorReader) Read(_ []byte) (int, error) {
	return 0, r.err
}
