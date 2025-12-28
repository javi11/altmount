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
	reader        *io.PipeReader
	writer        *io.PipeWriter
	once          sync.Once
	limitedReader io.Reader // Cached limited reader to prevent multiple LimitReader wraps
	mx            sync.Mutex
	closed        bool // Tracks if segment has been closed
}

func (s *segment) GetReader() io.Reader {
	s.once.Do(func() {
		// Skip to Start position
		if s.Start > 0 {
			// Seek to the start of the segment
			_, _ = io.CopyN(io.Discard, s.reader, s.Start)
		}

		// Create LimitReader once - this ensures the limit is enforced correctly
		// across multiple Read() calls in usenet_reader.go
		// Without this, each GetReader() call would create a NEW LimitReader with
		// the full limit, allowing reading beyond the intended End offset
		s.limitedReader = io.LimitReader(s.reader, s.End-s.Start+1)
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
	}

	if s.writer != nil {
		e2 = s.writer.Close()
	}

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
	}

	if s.writer != nil {
		e2 = s.writer.CloseWithError(err)
	}

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
	return sw.s.Close()
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

	var e1, e2 error
	if sw.s.reader != nil {
		e1 = sw.s.reader.CloseWithError(err)
	}
	if sw.s.writer != nil {
		e2 = sw.s.writer.CloseWithError(err)
	}

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
