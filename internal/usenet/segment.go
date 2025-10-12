package usenet

import (
	"errors"
	"io"
	"sync"

	"github.com/acomagu/bufpipe"
)

type Segment struct {
	Id    string
	Start int64
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
	mu       sync.RWMutex
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

	r.current += 1
	r.mu.Unlock()

	return r.Get()
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
	Id          string
	Start       int64
	End         int64
	SegmentSize int64
	groups      []string
	reader      *bufpipe.PipeReader
	writer      *bufpipe.PipeWriter
	once        sync.Once
	mx          sync.Mutex
}

func (s *segment) GetReader() io.Reader {
	s.once.Do(func() {
		if s.Start > 0 {
			// Seek to the start of the segment
			_, _ = io.CopyN(io.Discard, s.reader, s.Start)
		}
	})

	// Limit reader to exact byte range: (End - Start + 1)
	// After skipping Start bytes, we read from Start to End inclusive
	return io.LimitReader(s.reader, s.End-s.Start+1)
}

func (s *segment) Close() error {
	s.mx.Lock()
	defer s.mx.Unlock()

	var e1, e2 error

	if s.reader != nil {
		e1 = s.reader.Close()
		s.reader = nil
	}

	if s.writer != nil {
		e2 = s.writer.Close()
		s.writer = nil
	}

	return errors.Join(e1, e2)
}

func (s *segment) Writer() io.Writer {
	return s.writer
}

func (s *segment) ID() string {
	return s.Id
}

func (s *segment) Groups() []string {
	return s.groups
}
