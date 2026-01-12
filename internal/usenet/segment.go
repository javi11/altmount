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

// segment represents a downloaded usenet segment with buffered storage.
// Unlike the previous io.Pipe-based approach, this uses a bytes.Buffer
// which allows downloads to complete without blocking on reader consumption.
// This releases NNTP connections immediately after download completes.
type segment struct {
	Id          string
	Start       int64
	End         int64
	SegmentSize int64
	groups      []string

	// Buffered storage - replaces io.Pipe for non-blocking downloads
	// Buffer is allocated lazily on first Writer() call to avoid memory bloat
	// when creating many segments upfront (e.g., for large file ranges).
	buffer     *bytes.Buffer
	bufferOnce sync.Once     // For lazy buffer allocation
	ready      chan struct{} // Closed when download completes

	// Reader state
	once          sync.Once
	cachedReader  io.Reader // Cached reader instance
	downloadErr   error     // Stores download error for explicit retrieval
	closed        bool      // Tracks if segment has been closed
	mx            sync.Mutex
}

// newSegment creates a new segment without allocating a buffer.
// The buffer is allocated lazily on first Writer() call to avoid memory bloat
// when creating many segments upfront for large file ranges.
func newSegment(id string, start, end, segmentSize int64, groups []string) *segment {
	return &segment{
		Id:          id,
		Start:       start,
		End:         end,
		SegmentSize: segmentSize,
		groups:      groups,
		buffer:      nil, // Allocated lazily in Writer()
		ready:       make(chan struct{}),
	}
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

// segmentReader wraps buffer reading with limit and error awareness.
type segmentReader struct {
	s         *segment
	reader    *bytes.Reader
	remaining int64
}

func (r *segmentReader) Read(p []byte) (n int, err error) {
	// Always check for download error first - this takes priority over EOF
	if downloadErr := r.s.GetDownloadError(); downloadErr != nil {
		return 0, downloadErr
	}

	if r.remaining <= 0 {
		return 0, io.EOF
	}

	// Limit read to remaining bytes
	toRead := int64(len(p))
	if toRead > r.remaining {
		toRead = r.remaining
	}

	n, err = r.reader.Read(p[:toRead])
	r.remaining -= int64(n)

	// Check for download error after read
	if err != nil && err != io.EOF {
		if downloadErr := r.s.GetDownloadError(); downloadErr != nil {
			return n, downloadErr
		}
	}

	if r.remaining <= 0 && err == nil {
		err = io.EOF
	}

	return n, err
}

// GetReader returns a reader for the segment data.
// It waits for the download to complete before returning.
// This is safe to call multiple times - it will return the same reader.
func (s *segment) GetReader() io.Reader {
	s.once.Do(func() {
		// Wait for download to complete
		<-s.ready

		s.mx.Lock()
		defer s.mx.Unlock()

		if s.buffer == nil {
			s.cachedReader = &segmentReader{
				s:         s,
				reader:    bytes.NewReader(nil),
				remaining: 0,
			}
			return
		}

		// Create a reader from the buffer data
		data := s.buffer.Bytes()

		// Calculate the read window
		readStart := s.Start
		readEnd := s.End

		// Validate bounds
		if readStart < 0 {
			readStart = 0
		}
		if readEnd >= int64(len(data)) {
			readEnd = int64(len(data)) - 1
		}
		if readStart > readEnd {
			// Invalid range, return empty reader
			s.cachedReader = &segmentReader{
				s:         s,
				reader:    bytes.NewReader(nil),
				remaining: 0,
			}
			return
		}

		// Create reader starting at the correct offset
		remaining := readEnd - readStart + 1
		reader := bytes.NewReader(data[readStart:])

		s.cachedReader = &segmentReader{
			s:         s,
			reader:    reader,
			remaining: remaining,
		}
	})

	return s.cachedReader
}

// Close releases segment resources and returns the buffer to the pool.
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

	// Signal ready in case download never completed
	select {
	case <-s.ready:
		// Already closed
	default:
		close(s.ready)
	}

	// Clear cached reader to break slice reference to buffer data.
	// This is critical for GC to collect the underlying byte array.
	s.cachedReader = nil

	// Return buffer to pool (no-op, but clears reference for GC)
	if s.buffer != nil {
		putBuffer(s.buffer)
		s.buffer = nil
	}

	return nil
}

// CloseWithError closes the segment with an error, propagating it to readers.
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

	// Store the download error for readers
	if err != nil && s.downloadErr == nil {
		s.downloadErr = err
	}

	// Signal ready to unblock any waiting readers
	select {
	case <-s.ready:
		// Already closed
	default:
		close(s.ready)
	}

	// Clear cached reader to break slice reference to buffer data.
	// This is critical for GC to collect the underlying byte array.
	s.cachedReader = nil

	// Return buffer to pool (no-op, but clears reference for GC)
	if s.buffer != nil {
		putBuffer(s.buffer)
		s.buffer = nil
	}

	return nil
}

// segmentWriter provides a non-blocking writer for segment downloads.
// Writes go directly to the buffer without blocking on reader consumption.
type segmentWriter struct {
	s *segment
}

func (sw *segmentWriter) Write(p []byte) (n int, err error) {
	sw.s.mx.Lock()
	defer sw.s.mx.Unlock()

	if sw.s.closed {
		return 0, io.ErrClosedPipe
	}

	if sw.s.buffer == nil {
		return 0, io.ErrClosedPipe
	}

	// Write directly to buffer - never blocks!
	return sw.s.buffer.Write(p)
}

// Close signals that the download is complete.
// This unblocks any readers waiting for data.
func (sw *segmentWriter) Close() error {
	if sw.s == nil {
		return nil
	}

	sw.s.mx.Lock()
	defer sw.s.mx.Unlock()

	// Signal download complete
	select {
	case <-sw.s.ready:
		// Already closed
	default:
		close(sw.s.ready)
	}

	return nil
}

// CloseWithError closes the writer with an error.
// Unlike segment.CloseWithError, this does NOT release the buffer - it only
// signals that the download is complete (with an error) so readers can see the error.
// The buffer is retained so GetReader can still access any partial data and the error.
func (sw *segmentWriter) CloseWithError(err error) error {
	if sw.s == nil {
		return nil
	}

	sw.s.mx.Lock()
	defer sw.s.mx.Unlock()

	// Store the download error
	if err != nil && sw.s.downloadErr == nil {
		sw.s.downloadErr = err
	}

	// Signal ready to unblock readers - they will see the error
	select {
	case <-sw.s.ready:
		// Already closed
	default:
		close(sw.s.ready)
	}

	// Note: We do NOT set closed=true or release the buffer here.
	// The segment remains readable (will return the stored error).
	// Buffer is released when segment.Close() is called.

	return nil
}

// Writer returns a non-blocking writer for downloading segment data.
// The writer writes to an internal buffer, releasing the NNTP connection
// immediately when the download completes (unlike io.Pipe which blocks).
// The buffer is allocated lazily on first call to avoid memory bloat.
func (s *segment) Writer() io.Writer {
	// Lazily allocate buffer on first Writer() call
	s.bufferOnce.Do(func() {
		s.mx.Lock()
		defer s.mx.Unlock()
		if s.buffer == nil && !s.closed {
			s.buffer = getBuffer(s.SegmentSize)
		}
	})
	return &segmentWriter{s: s}
}

// ID returns the segment's message ID.
func (s *segment) ID() string {
	return s.Id
}

// Groups returns the newsgroups this segment is posted to.
func (s *segment) Groups() []string {
	return s.groups
}
