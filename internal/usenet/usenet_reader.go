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
	"sync/atomic"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/javi11/altmount/internal/slogutil"
	"github.com/javi11/nntppool/v4"
)

const (
	defaultMaxCacheSize = 32 * 1024 * 1024 // Default to 32MB
)

var (
	_ io.ReadCloser = &UsenetReader{}
)

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
	log            *slog.Logger
	wg             sync.WaitGroup
	cancel         context.CancelFunc
	rg             *segmentRange
	maxCacheSize   int64 // Maximum cache size in bytes
	init           chan any
	initDownload   sync.Once
	closeOnce      sync.Once
	totalBytesRead int64
	poolGetter     func() (*nntppool.Client, error) // Dynamic pool getter

	// Cache-budget-based download tracking
	nextToDownload int            // Index of next segment to schedule
	cachedBytes    int64          // Total bytes of downloaded-but-unread segments
	activeDownload int32          // Number of goroutines currently downloading
	downloadCond   *sync.Cond    // Condition variable for download coordination
	downloadWg     sync.WaitGroup // WaitGroup for download goroutines

	mu sync.Mutex
}

func NewUsenetReader(
	ctx context.Context,
	poolGetter func() (*nntppool.Client, error),
	rg *segmentRange,
	maxCacheSizeMB int,
) (*UsenetReader, error) {
	log := slog.Default().With("component", "usenet-reader")
	ctx, cancel := context.WithCancel(ctx)

	// Convert MB to bytes
	maxCacheSize := int64(maxCacheSizeMB) * 1024 * 1024
	if maxCacheSize <= 0 {
		maxCacheSize = defaultMaxCacheSize
	}

	ur := &UsenetReader{
		log:          log,
		cancel:       cancel,
		rg:           rg,
		init:         make(chan any, 1),
		maxCacheSize: maxCacheSize,
		poolGetter:   poolGetter,
	}
	ur.downloadCond = sync.NewCond(&ur.mu)

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

		// Unblock any pending reads waiting for data
		if b.rg != nil {
			b.rg.CloseSegments()
		}

		// Wake up the download manager if it's waiting
		b.mu.Lock()
		b.downloadCond.Broadcast()
		b.mu.Unlock()

		// Wait synchronously with timeout to prevent goroutine leaks
		done := make(chan struct{})
		go func() {
			b.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			b.mu.Lock()
			if b.rg != nil {
				_ = b.rg.Clear()
				b.rg = nil
			}
			b.mu.Unlock()
		case <-time.After(30 * time.Second):
			b.log.WarnContext(context.Background(), "Timeout waiting for downloads to complete during close, potential goroutine leak")
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
		b.mu.Lock()
		totalRead := b.totalBytesRead
		b.mu.Unlock()

		if b.isArticleNotFoundError(err) {
			if totalRead > 0 {
				return 0, &DataCorruptionError{
					UnderlyingErr: err,
					BytesRead:     totalRead,
				}
			} else {
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

		b.mu.Lock()
		b.totalBytesRead += int64(nn)
		totalRead := b.totalBytesRead
		b.mu.Unlock()

		if err != nil {
			if errors.Is(err, io.EOF) {
				// Segment fully read — account for freed cache bytes
				freedBytes := int64(s.DataLen())

				b.mu.Lock()
				rg := b.rg
				b.mu.Unlock()

				if rg == nil {
					return n, io.ErrClosedPipe
				}

				s, err = rg.Next()

				// Free cache budget and signal download manager
				b.mu.Lock()
				b.cachedBytes -= freedBytes
				if b.cachedBytes < 0 {
					b.cachedBytes = 0
				}
				b.downloadCond.Signal()
				b.mu.Unlock()

				if err != nil {
					if n > 0 {
						return n, nil
					}

					if b.isArticleNotFoundError(err) {
						if totalRead > 0 {
							return n, &DataCorruptionError{
								UnderlyingErr: err,
								BytesRead:     totalRead,
							}
						}
					}
					return n, io.EOF
				}
			} else {
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
	return errors.Is(err, nntppool.ErrArticleNotFound)
}

func (b *UsenetReader) GetBufferedOffset() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.rg == nil {
		return 0
	}

	if b.nextToDownload == 0 {
		return 0
	}

	idx := b.nextToDownload - 1
	s, err := b.rg.GetSegment(idx)
	if err != nil || s == nil {
		return 0
	}
	return s.Start + int64(s.SegmentSize)
}

// isPoolUnavailableError checks if the error indicates the pool is unavailable or shutdown
func (b *UsenetReader) isPoolUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection pool is shutdown") ||
		strings.Contains(errStr, "connection pool not available") ||
		strings.Contains(errStr, "NNTP connection pool not available")
}

// downloadSegmentWithRetry attempts to download a segment with retry logic for pool unavailability
func (b *UsenetReader) downloadSegmentWithRetry(ctx context.Context, seg *segment) ([]byte, error) {
	var resultBytes []byte
	err := retry.Do(
		func() error {
			attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			cp, err := b.poolGetter()
			if err != nil {
				return err
			}

			buf := bytes.NewBuffer(make([]byte, 0, seg.SegmentSize))
			result, err := cp.BodyStream(attemptCtx, seg.Id, buf)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					b.log.DebugContext(ctx, "Segment download attempt timed out after 30s", "segment_id", seg.Id)
				}

				var bytesWritten int64
				if result != nil {
					bytesWritten = int64(result.BytesDecoded)
				}

				if strings.Contains(err.Error(), "data corruption detected") {
					return &DataCorruptionError{
						UnderlyingErr: err,
						BytesRead:     bytesWritten,
					}
				}

				return err
			}

			resultBytes = buf.Bytes()
			return nil
		},
		retry.Attempts(10),
		retry.Delay(50*time.Millisecond),
		retry.MaxDelay(2*time.Second),
		retry.DelayType(retry.BackOffDelay),
		retry.RetryIf(func(err error) bool {
			if b.isArticleNotFoundError(err) {
				return false
			}
			return b.isPoolUnavailableError(err) || errors.Is(err, context.DeadlineExceeded)
		}),
		retry.OnRetry(func(n uint, err error) {
			b.log.DebugContext(ctx, "Pool unavailable or timeout, retrying segment download",
				"attempt", n+1,
				"segment_id", seg.Id,
				"error", err)
		}),
		retry.Context(ctx),
	)
	return resultBytes, err
}

func (b *UsenetReader) downloadManager(ctx context.Context) {
	select {
	case _, ok := <-b.init:
		if !ok {
			return
		}

		// Ensure any pending Wait() is woken up on context cancellation
		go func() {
			<-ctx.Done()
			b.mu.Lock()
			b.downloadCond.Broadcast()
			b.mu.Unlock()
		}()

		if b.rg.Len() == 0 {
			return
		}

		// Get average segment size for cache budget estimation
		s0, err := b.rg.GetSegment(0)
		if err != nil || s0 == nil {
			return
		}
		avgSegmentSize := s0.SegmentSize

		for ctx.Err() == nil {
			b.mu.Lock()
			if b.rg == nil {
				b.mu.Unlock()
				return
			}

			// Check if all segments have been scheduled
			totalSegments := b.rg.Len()
			if b.nextToDownload >= totalSegments {
				b.mu.Unlock()
				break
			}

			// Check if cache budget allows more downloads
			// Use estimated size (avgSegmentSize) for scheduling since we don't know actual size yet
			estimatedNewBytes := b.cachedBytes + int64(atomic.LoadInt32(&b.activeDownload))*avgSegmentSize
			if estimatedNewBytes+avgSegmentSize > b.maxCacheSize {
				// Cache full — wait for reader to consume segments
				if ctx.Err() != nil {
					b.mu.Unlock()
					break
				}
				b.downloadCond.Wait()
				b.mu.Unlock()
				continue
			}

			// Schedule next segment for download
			idx := b.nextToDownload
			b.nextToDownload++
			b.mu.Unlock()

			seg, err := b.rg.GetSegment(idx)
			if err != nil || seg == nil {
				continue
			}

			atomic.AddInt32(&b.activeDownload, 1)
			b.downloadWg.Add(1)
			go func(segIdx int, s *segment) {
				defer func() {
					atomic.AddInt32(&b.activeDownload, -1)
					b.downloadWg.Done()

					if p := recover(); p != nil {
						b.log.ErrorContext(ctx, "Panic in download task:", "panic", p)
						s.SetError(fmt.Errorf("panic in download task: %v", p))
					}

					// Signal manager that a download finished (frees estimated budget)
					b.mu.Lock()
					b.downloadCond.Signal()
					b.mu.Unlock()
				}()

				taskCtx := slogutil.With(ctx, "segment_id", s.Id, "segment_idx", segIdx)
				data, err := b.downloadSegmentWithRetry(taskCtx, s)

				if err != nil {
					s.SetError(err)
				} else {
					s.SetData(data)

					// Add actual bytes to cache budget
					b.mu.Lock()
					b.cachedBytes += int64(len(data))
					b.mu.Unlock()
				}
			}(idx, seg)
		}

		// Wait for all in-flight downloads to complete
		b.downloadWg.Wait()

	case <-ctx.Done():
		return
	}
}
