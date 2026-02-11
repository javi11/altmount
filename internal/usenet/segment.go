package usenet

import (
	"bytes"
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

	once          sync.Once  // Guards GetReader initialization
	limitedReader io.Reader  // Cached limited reader
	mx            sync.Mutex // Protects released flag
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

// GetReader returns a reader for the segment data.
// Blocks until data is available or an error is set.
// The reader is limited to the range [Start, End] within the segment.
func (s *segment) GetReader() io.Reader {
	s.once.Do(func() {
		// Wait for data to be ready
		<-s.dataReady

		// Check for error first
		s.mx.Lock()
		err := s.dataErr
		data := s.data
		s.mx.Unlock()

		if err != nil {
			s.limitedReader = &errorReader{err: err}
			return
		}

		// Create a reader over the full data
		fullReader := bytes.NewReader(data)

		// Skip to Start position
		if s.Start > 0 {
			_, seekErr := fullReader.Seek(s.Start, io.SeekStart)
			if seekErr != nil {
				s.mx.Lock()
				if s.dataErr == nil {
					s.dataErr = seekErr
				}
				s.mx.Unlock()
				s.limitedReader = &errorReader{err: seekErr}
				return
			}
		}

		// Create LimitReader for the range [Start, End]
		s.limitedReader = io.LimitReader(fullReader, s.End-s.Start+1)
	})

	return s.limitedReader
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
