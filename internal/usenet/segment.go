package usenet

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/djherbis/buffer"
	"github.com/djherbis/nio/v3"
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

func (r *segmentRange) Get() (*segment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.current >= len(r.segments) {
		return nil, ErrSegmentLimit
	}

	return r.segments[r.current], nil
}

func (r *segmentRange) Next() (*segment, error) {
	r.mu.Lock()
	if r.current >= len(r.segments) {
		r.mu.Unlock()
		return nil, ErrSegmentLimit
	}

	// Ignore close errors
	_ = r.segments[r.current].Close()
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
			_ = s.CloseWithError(err)
		}
	}
}

func (r *segmentRange) CloseSegments() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.segments {
		if s != nil {
			_ = s.Close()
		}
	}
}

func (r *segmentRange) Clear() error {
	for _, s := range r.segments {
		if err := s.Close(); err != nil {
			return err
		}
	}

	r.segments = nil

	return nil
}

type segment struct {
	Id            string
	Start         int64
	End           int64
	SegmentSize   int64
	groups        []string
	reader        *nio.PipeReader
	writer        *nio.PipeWriter
	buf           buffer.Buffer // Bounded buffer for backpressure
	bufferSize    int64         // Size for lazy buffer initialization
	once          sync.Once
	bufferOnce    sync.Once     // For lazy buffer initialization
	limitedReader io.Reader     // Cached limited reader to prevent multiple LimitReader wraps
	mx            sync.Mutex
	closed        bool  // Tracks if segment has been closed
	downloadErr   error // Stores download error for explicit retrieval
}

// initBuffer lazily initializes the bounded buffer for this segment.
// This is called by the download manager before downloading to avoid
// allocating buffers for all segments upfront.
func (s *segment) initBuffer() {
	s.bufferOnce.Do(func() {
		s.mx.Lock()
		defer s.mx.Unlock()

		// Don't initialize if already closed
		if s.closed {
			return
		}

		// Create bounded buffer for backpressure
		buf := buffer.New(s.bufferSize)
		r, w := nio.Pipe(buf)
		s.buf = buf
		s.reader = r
		s.writer = w
	})
}

// isBufferInitialized returns true if the buffer has been initialized
func (s *segment) isBufferInitialized() bool {
	s.mx.Lock()
	defer s.mx.Unlock()
	return s.buf != nil
}

// SetDownloadError stores the download error for later retrieval.
// Uses first-write-wins semantics to preserve the original error.
func (s *segment) SetDownloadError(err error) {
	if s == nil || err == nil {
		return
	}
	s.mx.Lock()
	defer s.mx.Unlock()
	if s.downloadErr == nil {
		s.downloadErr = err
	}
}

// GetDownloadError returns any download error that occurred.
func (s *segment) GetDownloadError() error {
	if s == nil {
		return nil
	}
	s.mx.Lock()
	defer s.mx.Unlock()
	return s.downloadErr
}

// errorAwareReader wraps a reader and checks segment error state.
// This ensures that download errors are always propagated to the reader,
// even if the error occurred during pipe operations.
type errorAwareReader struct {
	s      *segment
	reader io.Reader
}

func (r *errorAwareReader) Read(p []byte) (n int, err error) {
	// Check for stored download error before reading
	if downloadErr := r.s.GetDownloadError(); downloadErr != nil {
		return 0, downloadErr
	}

	n, err = r.reader.Read(p)

	// On any read error (except EOF), check if there's a more specific download error
	if err != nil && err != io.EOF {
		if downloadErr := r.s.GetDownloadError(); downloadErr != nil {
			return n, downloadErr
		}
	}

	return n, err
}

func (s *segment) GetReader() io.Reader {
	// Ensure buffer is initialized before reading
	// This is safe because initBuffer uses sync.Once and is idempotent
	s.initBuffer()

	s.once.Do(func() {
		// Skip to Start position
		if s.Start > 0 {
			// Seek to the start of the segment
			_, err := io.CopyN(io.Discard, s.reader, s.Start)
			if err != nil && err != io.EOF {
				// Store the error for later retrieval
				s.mx.Lock()
				if s.downloadErr == nil {
					s.downloadErr = err
				}
				s.mx.Unlock()
			}
		}

		// Create LimitReader once - this ensures the limit is enforced correctly
		// across multiple Read() calls in usenet_reader.go
		// Without this, each GetReader() call would create a NEW LimitReader with
		// the full limit, allowing reading beyond the intended End offset
		limited := io.LimitReader(s.reader, s.End-s.Start+1)

		// Wrap in errorAwareReader to ensure download errors are always propagated
		s.limitedReader = &errorAwareReader{s: s, reader: limited}
	})

	return s.limitedReader
}

func (s *segment) Close() error {
	if s == nil {
		return nil
	}

	s.mx.Lock()
	defer s.mx.Unlock()

	// Prevent multiple closes
	if s.closed {
		return nil
	}
	s.closed = true

	var e1, e2 error

	if s.reader != nil {
		e1 = s.reader.Close()
		s.reader = nil
	}

	if s.writer != nil {
		e2 = s.writer.Close()
		s.writer = nil
	}

	// Reset the buffer to release its internal storage and prevent memory leaks
	if s.buf != nil {
		s.buf.Reset()
		s.buf = nil
	}

	// Clear remaining references for aggressive GC
	s.limitedReader = nil
	s.groups = nil

	return errors.Join(e1, e2)
}

func (s *segment) CloseWithError(err error) error {
	if s == nil {
		return nil
	}

	s.mx.Lock()
	defer s.mx.Unlock()

	// Prevent multiple closes
	if s.closed {
		return nil
	}
	s.closed = true

	var e1, e2 error

	if s.reader != nil {
		e1 = s.reader.CloseWithError(err)
		s.reader = nil
	}

	if s.writer != nil {
		e2 = s.writer.CloseWithError(err)
		s.writer = nil
	}

	// Reset the buffer to release its internal storage and prevent memory leaks
	if s.buf != nil {
		s.buf.Reset()
		s.buf = nil
	}

	// Clear remaining references for aggressive GC
	s.limitedReader = nil
	s.groups = nil

	return errors.Join(e1, e2)
}

// safeWriter wraps the segment writer and returns error if closed
type safeWriter struct {
	s *segment
}

func (sw *safeWriter) Write(p []byte) (n int, err error) {
	sw.s.mx.Lock()
	closed := sw.s.closed
	writer := sw.s.writer
	sw.s.mx.Unlock()

	if closed || writer == nil {
		return 0, io.ErrClosedPipe
	}

	return writer.Write(p)
}

func (sw *safeWriter) Close() error {
	if sw.s == nil {
		return nil
	}
	sw.s.mx.Lock()
	defer sw.s.mx.Unlock()

	// Only close the writer pipe to signal EOF to readers
	// The reader pipe stays open until segment.Close() is called
	if sw.s.writer != nil {
		return sw.s.writer.Close()
	}
	return nil
}

func (sw *safeWriter) CloseWithError(err error) error {
	if sw.s == nil {
		return nil
	}

	sw.s.mx.Lock()
	defer sw.s.mx.Unlock()

	if sw.s.closed {
		return nil
	}
	sw.s.closed = true

	// Store the download error for explicit retrieval by the reader
	if err != nil && sw.s.downloadErr == nil {
		sw.s.downloadErr = err
	}

	var e1, e2 error
	if sw.s.reader != nil {
		e1 = sw.s.reader.CloseWithError(err)
		sw.s.reader = nil
	}
	if sw.s.writer != nil {
		e2 = sw.s.writer.CloseWithError(err)
		sw.s.writer = nil
	}

	// Reset the buffer to release its internal storage and prevent memory leaks
	if sw.s.buf != nil {
		sw.s.buf.Reset()
		sw.s.buf = nil
	}

	// Clear remaining references for aggressive GC
	sw.s.limitedReader = nil
	sw.s.groups = nil

	return errors.Join(e1, e2)
}

func (s *segment) Writer() io.Writer {
	return &safeWriter{s: s}
}

func (s *segment) ID() string {
	return s.Id
}

func (s *segment) Groups() []string {
	return s.groups
}
