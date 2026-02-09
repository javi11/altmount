package usenet

import (
	"context"
	"fmt"
	"io"

	"github.com/javi11/nntppool/v4"
	"github.com/javi11/nzbparser"
)

// SequentialReader provides sequential reading across multiple NZB segments
// This is a simple reader that reads segments one by one without caching or concurrency
// Useful for cases where you need to stream through all segments of a file sequentially
type SequentialReader struct {
	ctx            context.Context
	segments       []nzbparser.NzbSegment
	groups         []string
	pool           *nntppool.Client
	currentIndex   int
	currentReader  io.ReadCloser
	currentSegment nzbparser.NzbSegment
}

// NewSequentialReader creates a new sequential reader for the given NZB segments
// It reads segments one by one in order, automatically transitioning to the next segment
// when the current one is exhausted
func NewSequentialReader(
	ctx context.Context,
	segments []nzbparser.NzbSegment,
	groups []string,
	pool *nntppool.Client,
) (*SequentialReader, error) {
	if len(segments) == 0 {
		return nil, fmt.Errorf("no segments provided")
	}

	if pool == nil {
		return nil, fmt.Errorf("connection pool is required")
	}

	sr := &SequentialReader{
		ctx:          ctx,
		segments:     segments,
		groups:       groups,
		pool:         pool,
		currentIndex: -1, // Start before first segment
	}

	// Open the first segment
	if err := sr.openNextSegment(); err != nil {
		return nil, fmt.Errorf("failed to open first segment: %w", err)
	}

	return sr, nil
}

// Read reads data from the current segment, automatically moving to the next segment
// when the current one is exhausted
func (sr *SequentialReader) Read(p []byte) (int, error) {
	if sr.currentReader == nil {
		return 0, io.EOF
	}

	for {
		// Try to read from current segment
		n, err := sr.currentReader.Read(p)
		if n > 0 {
			return n, nil
		}

		// Handle errors
		if err == io.EOF {
			// Current segment exhausted, try next segment
			if err := sr.openNextSegment(); err != nil {
				// No more segments or error opening next
				return 0, io.EOF
			}
			// Loop to read from new segment
			continue
		}

		// Other error
		if err != nil {
			return 0, fmt.Errorf("error reading segment %s: %w", sr.currentSegment.ID, err)
		}

		// n == 0 and no error (shouldn't happen, but handle gracefully)
		// Try next segment
		if err := sr.openNextSegment(); err != nil {
			return 0, io.EOF
		}
	}
}

// openNextSegment closes the current segment reader and opens the next one
func (sr *SequentialReader) openNextSegment() error {
	// Close current segment if any
	if sr.currentReader != nil {
		_ = sr.currentReader.Close()
		sr.currentReader = nil
	}

	// Move to next segment
	sr.currentIndex++

	// Check if we have more segments
	if sr.currentIndex >= len(sr.segments) {
		return io.EOF
	}

	// Get next segment
	segment := sr.segments[sr.currentIndex]
	sr.currentSegment = segment

	r, w := io.Pipe()
	go func() {
		defer w.Close()
		_, err := sr.pool.BodyStream(sr.ctx, segment.ID, w)
		if err != nil {
			_ = r.CloseWithError(err)
		}
	}()
	sr.currentReader = r
	return nil
}

// Close closes the current segment reader and releases resources
func (sr *SequentialReader) Close() error {
	if sr.currentReader != nil {
		err := sr.currentReader.Close()
		sr.currentReader = nil
		return err
	}
	return nil
}
