package usenet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/javi11/altmount/internal/slogutil"
	"github.com/javi11/nntppool/v3"
	"github.com/sourcegraph/conc/pool"
)

const (
	defaultDownloadWorkers = 15
	// maxPooledBufferSize is the maximum buffer size to return to the pool.
	// Buffers larger than this are discarded to prevent excessive memory retention.
	maxPooledBufferSize = 2 * 1024 * 1024 // 2MB
)

var (
	_ io.ReadCloser = &UsenetReader{}

	// segmentBufferPool reuses buffers for segment downloads to reduce allocations.
	// Downloading to a buffer first releases the NNTP connection quickly,
	// preventing deadlocks when workers complete out-of-order.
	segmentBufferPool = sync.Pool{
		New: func() any {
			return new(bytes.Buffer)
		},
	}
)

func getBuffer() *bytes.Buffer {
	buf := segmentBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func putBuffer(buf *bytes.Buffer) {
	// Only return reasonably-sized buffers to the pool
	if buf.Cap() <= maxPooledBufferSize {
		segmentBufferPool.Put(buf)
	}

	buf = nil
}

type DataCorruptionError struct {
	UnderlyingErr error
	BytesRead     int64
	NoRetry       bool
}

func (e *DataCorruptionError) Error() string {
	return e.UnderlyingErr.Error()
}

func (e *DataCorruptionError) Unwrap() error {
	return e.UnderlyingErr
}

type UsenetReader struct {
	log                *slog.Logger
	wg                 sync.WaitGroup
	cancel             context.CancelFunc
	rg                 *segmentRange
	maxDownloadWorkers int
	init               chan any
	initDownload       sync.Once
	closeOnce          sync.Once
	totalBytesRead     int64
	poolGetter         func() (nntppool.NNTPClient, error) // Dynamic pool getter

	mu sync.Mutex
}

func NewUsenetReader(
	ctx context.Context,
	poolGetter func() (nntppool.NNTPClient, error),
	rg *segmentRange,
	maxDownloadWorkers int,
) (*UsenetReader, error) {
	log := slog.Default().With("component", "usenet-reader")
	ctx, cancel := context.WithCancel(ctx)

	ur := &UsenetReader{
		log:                log,
		cancel:             cancel,
		rg:                 rg,
		init:               make(chan any, 1),
		maxDownloadWorkers: maxDownloadWorkers,
		poolGetter:         poolGetter,
	}

	// Will start go routine pool with max download workers that will fill the cache

	ur.wg.Add(1)
	go func() {
		defer ur.wg.Done()
		ur.downloadManager(ctx)
	}()

	return ur, nil
}

// Start triggers the background download process manually.
// This is useful for pre-fetching data before the first Read call.
func (b *UsenetReader) Start() {
	b.initDownload.Do(func() {
		// Use select to avoid blocking or panicking if closed
		select {
		case b.init <- struct{}{}:
		default:
		}
	})
}

func (b *UsenetReader) Close() error {
	b.closeOnce.Do(func() {
		b.cancel()

		// Drain and close init channel safely
		select {
		case <-b.init:
		default:
		}
		close(b.init)

		// Unblock any pending writes/reads
		if b.rg != nil {
			b.rg.CloseSegments()
		}

		// Wait synchronously with timeout to prevent goroutine leaks
		// Use a separate goroutine to detect when cleanup completes
		done := make(chan struct{})
		go func() {
			b.wg.Wait()
			close(done)
		}()

		// Wait for cleanup with reasonable timeout
		select {
		case <-done:
			// Cleanup completed successfully
			b.mu.Lock()
			if b.rg != nil {
				_ = b.rg.Clear()
				b.rg = nil
			}
			b.mu.Unlock()
		case <-time.After(30 * time.Second):
			// Timeout waiting for downloads to complete
			// This prevents hanging but logs the issue
			b.log.WarnContext(context.Background(), "Timeout waiting for downloads to complete during close, potential goroutine leak")
			// Still attempt to clear resources
			b.mu.Lock()
			if b.rg != nil {
				_ = b.rg.Clear()
				b.rg = nil
			}
			b.mu.Unlock()
		}
	})

	return nil
}

// Read reads len(p) byte from the Buffer starting at the current offset.
// It returns the number of bytes read and an error if any.
// Returns io.EOF error if pointer is at the end of the Buffer.
func (b *UsenetReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	b.initDownload.Do(func() {
		// Use select to avoid blocking or panicking if closed
		select {
		case b.init <- struct{}{}:
		default:
		}
	})

	b.mu.Lock()
	rg := b.rg
	b.mu.Unlock()

	if rg == nil {
		return 0, io.ErrClosedPipe
	}

	s, err := rg.Get()
	if err != nil {
		// Check if this is an article not found error
		b.mu.Lock()
		totalRead := b.totalBytesRead
		b.mu.Unlock()

		if b.isArticleNotFoundError(err) {
			if totalRead > 0 {
				// We read some data before failing - this is partial content
				return 0, &DataCorruptionError{
					UnderlyingErr: err,
					BytesRead:     totalRead,
				}
			} else {
				// No data read at all - this is corrupted/missing
				return 0, &DataCorruptionError{
					UnderlyingErr: err,
					BytesRead:     0,
				}
			}
		}
		return 0, io.EOF
	}

	n := 0
	for n < len(p) {
		nn, err := s.GetReader().Read(p[n:])
		n += nn

		// Track total bytes read
		b.mu.Lock()
		b.totalBytesRead += int64(nn)
		totalRead := b.totalBytesRead
		b.mu.Unlock()

		if err != nil {
			if errors.Is(err, io.EOF) {
				// Segment is fully read, remove it from the cache
				b.mu.Lock()
				rg := b.rg
				b.mu.Unlock()

				if rg == nil {
					return n, io.ErrClosedPipe
				}

				s, err = rg.Next()
				if err != nil {
					if n > 0 {
						return n, nil
					}

					// Check if this is an article not found error for next segment
					if b.isArticleNotFoundError(err) {
						if totalRead > 0 {
							// Return what we have read so far and the article error
							return n, &DataCorruptionError{
								UnderlyingErr: err,
								BytesRead:     totalRead,
							}
						}
					}
					return n, io.EOF
				}
			} else {
				// Check if this is an article not found error
				if b.isArticleNotFoundError(err) {
					return n, &DataCorruptionError{
						UnderlyingErr: err,
						BytesRead:     totalRead,
					}
				}
				return n, err
			}
		}
	}

	return n, nil
}

// isArticleNotFoundError checks if the error indicates articles were not found in providers
func (b *UsenetReader) isArticleNotFoundError(err error) bool {
	var articleErr *nntppool.ArticleNotFoundError
	return errors.As(err, &articleErr)
}

func (b *UsenetReader) GetBufferedOffset() int64 {
	// With the simplified download manager that eagerly downloads all segments,
	// we report the total size as the buffered offset since all segments are queued.
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.rg == nil {
		return 0
	}

	segmentCount := b.rg.Len()
	if segmentCount == 0 {
		return 0
	}

	// Return the end offset of the last segment
	lastSegment, err := b.rg.GetSegment(segmentCount - 1)
	if err != nil || lastSegment == nil {
		return 0
	}
	return lastSegment.Start + int64(lastSegment.SegmentSize)
}

// downloadSegmentWithRetry attempts to download a segment with retry logic.
// It downloads to a temporary buffer first to release the NNTP connection quickly,
// then copies the data to the segment's pipe. This prevents deadlocks when
// workers complete out-of-order and block on pipe writes while holding connections.
func (b *UsenetReader) downloadSegmentWithRetry(ctx context.Context, segment *segment) error {
	return retry.Do(
		func() error {
			// Get buffer from pool for intermediate storage
			buf := getBuffer()
			defer putBuffer(buf)

			// Pre-allocate buffer if segment size is known
			if segment.SegmentSize > 0 {
				buf.Grow(int(segment.SegmentSize))
			}

			// Get current pool
			cp, err := b.poolGetter()
			if err != nil {
				return err
			}

			// Phase 1: Download to buffer (releases NNTP connection quickly)
			err = cp.Body(ctx, segment.Id, buf)
			if err != nil {
				// The segment is closed, so we can return nil - no retry needed
				if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}

			// Phase 2: Write from buffer to pipe (can block without holding NNTP connection)
			// Use WriteTo for efficient single-write operation instead of chunked io.Copy
			_, err = buf.WriteTo(segment.Writer())
			if err != nil {
				// The segment is closed, so we can return nil - no retry needed
				if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}

			return nil
		},
		retry.Attempts(5),
		retry.Delay(15*time.Millisecond),
		retry.MaxJitter(10*time.Millisecond),
		retry.MaxDelay(2*time.Second),
		retry.DelayType(retry.BackOffDelay),
		retry.RetryIf(func(err error) bool {
			if b.isArticleNotFoundError(err) || ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return false
			}
			// Retry on pool-related errors OR timeout errors
			return true
		}),
		retry.OnRetry(func(n uint, err error) {
			b.log.DebugContext(ctx, "Download error, retrying segment download",
				"attempt", n+1,
				"segment_id", segment.Id,
				"error", err)
		}),
		retry.Context(ctx),
	)
}

func (b *UsenetReader) downloadManager(ctx context.Context) {
	select {
	case _, ok := <-b.init:
		if !ok {
			return
		}

		downloadWorkers := b.maxDownloadWorkers
		if downloadWorkers == 0 {
			downloadWorkers = defaultDownloadWorkers
		}

		segmentCount := b.rg.Len()
		if segmentCount == 0 {
			return
		}

		p := pool.New().
			WithMaxGoroutines(downloadWorkers).
			WithContext(ctx)

		// Queue all segment downloads - worker pool limits concurrency
		for i := 0; i < segmentCount; i++ {
			segmentIdx := i
			p.Go(func(c context.Context) error {
				return b.downloadSegment(c, segmentIdx)
			})
		}

		if err := p.Wait(); err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return
			}
			if strings.Contains(err.Error(), "closed pipe") {
				b.log.DebugContext(ctx, "Suppressed closed pipe error during shutdown", "error", err)
				return
			}
			b.log.DebugContext(ctx, "Error downloading segments:", "error", err)
		}
	case <-ctx.Done():
		return
	}
}

// downloadSegment handles downloading a single segment by index
func (b *UsenetReader) downloadSegment(ctx context.Context, segmentIdx int) (err error) {
	defer func() {
		if p := recover(); p != nil {
			b.log.ErrorContext(ctx, "Panic in download task:", "panic", p)
			err = fmt.Errorf("panic in download task: %v", p)
		}
	}()

	b.mu.Lock()
	rg := b.rg
	b.mu.Unlock()

	if rg == nil {
		return nil
	}

	s, err := rg.GetSegment(segmentIdx)
	if err != nil || s == nil {
		return nil
	}

	w := s.Writer()
	if w == nil {
		return fmt.Errorf("segment writer is nil")
	}

	taskCtx := slogutil.With(ctx, "segment_id", s.Id, "segment_idx", segmentIdx)
	err = b.downloadSegmentWithRetry(taskCtx, s)
	if err != nil {
		if cew, ok := w.(interface{ CloseWithError(error) error }); ok {
			_ = cew.CloseWithError(err)
		} else if cw, ok := w.(io.Closer); ok {
			_ = cw.Close()
		}
	} else {
		if cw, ok := w.(io.Closer); ok {
			_ = cw.Close()
		}
	}

	return err
}
