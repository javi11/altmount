package usenet

import (
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
	"github.com/javi11/nntppool/v2"
	"github.com/sourcegraph/conc/pool"
)

const (
	defaultMaxCacheSize    = 32 * 1024 * 1024 // Default to 32MB
	defaultDownloadWorkers = 15
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
	log                *slog.Logger
	wg                 sync.WaitGroup
	cancel             context.CancelFunc
	rg                 *segmentRange
	maxDownloadWorkers int
	maxCacheSize       int64 // Maximum cache size in bytes
	init               chan any
	initDownload       sync.Once
	closeOnce          sync.Once
	totalBytesRead     int64
	poolGetter         func() (nntppool.UsenetConnectionPool, error) // Dynamic pool getter

	// Dynamic download tracking
	nextToDownload      int          // Index of next segment to download
	downloadingSegments map[int]bool // Track which segments are being downloaded
	downloadCond        *sync.Cond   // Condition variable for download coordination

	mu sync.Mutex
}

func NewUsenetReader(
	ctx context.Context,
	poolGetter func() (nntppool.UsenetConnectionPool, error),
	rg *segmentRange,
	maxDownloadWorkers int,
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
		log:                 log,
		cancel:              cancel,
		rg:                  rg,
		init:                make(chan any, 1),
		maxDownloadWorkers:  maxDownloadWorkers,
		maxCacheSize:        maxCacheSize,
		poolGetter:          poolGetter,
		nextToDownload:      0,
		downloadingSegments: make(map[int]bool),
	}
	ur.downloadCond = sync.NewCond(&ur.mu)

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
			if b.rg != nil {
				_ = b.rg.Clear()
				b.rg = nil
			}
		case <-time.After(30 * time.Second):
			// Timeout waiting for downloads to complete
			// This prevents hanging but logs the issue
			b.log.WarnContext(context.Background(), "Timeout waiting for downloads to complete during close, potential goroutine leak")
			// Still attempt to clear resources
			if b.rg != nil {
				_ = b.rg.Clear()
				b.rg = nil
			}
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

	s, err := b.rg.Get()
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
				s, err = b.rg.Next()

				// Signal that we've moved to the next segment (triggers more downloads)
				b.mu.Lock()
				b.downloadCond.Signal()
				b.mu.Unlock()

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
	return errors.Is(err, nntppool.ErrArticleNotFoundInProviders)
}

func (b *UsenetReader) GetBufferedOffset() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.nextToDownload == 0 || len(b.rg.segments) == 0 {
		return 0
	}

	idx := b.nextToDownload - 1
	if idx >= len(b.rg.segments) {
		idx = len(b.rg.segments) - 1
	}

	s := b.rg.segments[idx]
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
func (b *UsenetReader) downloadSegmentWithRetry(ctx context.Context, segment *segment) error {
	return retry.Do(
		func() error {
			// Create a per-attempt timeout context to prevent hanging on network/DNS issues
			attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			// Get current pool
			cp, err := b.poolGetter()
			if err != nil {
				return err
			}

			// Attempt download using the timeout context
			bytesWritten, err := cp.Body(attemptCtx, segment.Id, segment.Writer(), segment.groups)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					b.log.DebugContext(ctx, "Segment download attempt timed out after 30s", "segment_id", segment.Id)
				}

				if strings.Contains(err.Error(), "data corruption detected") {
					return &DataCorruptionError{
						UnderlyingErr: err,
						BytesRead:     bytesWritten,
					}
				}

				return err
			}

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
			// Retry on pool-related errors OR timeout errors
			return b.isPoolUnavailableError(err) || errors.Is(err, context.DeadlineExceeded)
		}),
		retry.OnRetry(func(n uint, err error) {
			b.log.DebugContext(ctx, "Pool unavailable or timeout, retrying segment download",
				"attempt", n+1,
				"segment_id", segment.Id,
				"error", err)
		}),
		retry.Context(ctx),
	)
}

func (b *UsenetReader) downloadManager(
	ctx context.Context,
) {
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

		downloadWorkers := b.maxDownloadWorkers
		if downloadWorkers == 0 {
			downloadWorkers = defaultDownloadWorkers
		}

		if len(b.rg.segments) == 0 {
			return
		}

		// Calculate max segments to download ahead based on cache size
		avgSegmentSize := b.rg.segments[0].SegmentSize
		maxSegmentsAhead := int(b.maxCacheSize / avgSegmentSize)
		if maxSegmentsAhead < 1 {
			maxSegmentsAhead = 1 // Always allow at least 1 segment
		}
		if maxSegmentsAhead > len(b.rg.segments) {
			maxSegmentsAhead = len(b.rg.segments)
		}

		// Limit concurrent downloads to prevent cache overflow
		if downloadWorkers > maxSegmentsAhead {
			downloadWorkers = maxSegmentsAhead
		}

		pool := pool.New().
			WithMaxGoroutines(downloadWorkers).
			WithContext(ctx)

		// Start continuous download monitoring
		for ctx.Err() == nil {
			b.mu.Lock()
			if b.rg == nil {
				b.mu.Unlock()
				return
			}
			// Get current reading position
			currentIndex := b.rg.GetCurrentIndex()

			// Calculate how many segments we should have downloaded
			targetDownload := currentIndex + maxSegmentsAhead
			if targetDownload > len(b.rg.segments) {
				targetDownload = len(b.rg.segments)
			}

			// Download segments that are not yet downloaded or downloading
			segmentsToQueue := []int{}
			for i := b.nextToDownload; i < targetDownload; i++ {
				// Check for context cancellation frequently during segment selection
				select {
				case <-ctx.Done():
					b.mu.Unlock()
					return
				default:
				}

				if !b.downloadingSegments[i] {
					b.downloadingSegments[i] = true
					segmentsToQueue = append(segmentsToQueue, i)
				}
			}
			// Update nextToDownload to reflect we've checked up to targetDownload
			if targetDownload > b.nextToDownload {
				b.nextToDownload = targetDownload
			}
			b.mu.Unlock()

			// Queue downloads for new segments
			for _, idx := range segmentsToQueue {
				if ctx.Err() != nil {
					break
				}

				segmentIdx := idx // Capture for closure
				b.mu.Lock()
				if b.rg == nil || segmentIdx >= len(b.rg.segments) {
					b.mu.Unlock()
					continue
				}
				s := b.rg.segments[segmentIdx]
				b.mu.Unlock()

				pool.Go(func(c context.Context) (err error) {
					defer func() {
						if p := recover(); p != nil {
							b.log.ErrorContext(ctx, "Panic in download task:", "panic", p)
							err = fmt.Errorf("panic in download task: %v", p)
						}

						// Mark download complete
						b.mu.Lock()
						delete(b.downloadingSegments, segmentIdx)
						b.downloadCond.Signal()
						b.mu.Unlock()
					}()

					w := s.Writer()
					if w == nil {
						return fmt.Errorf("segment writer is nil")
					}

					taskCtx := slogutil.With(ctx, "segment_id", s.Id, "segment_idx", segmentIdx)
					err = b.downloadSegmentWithRetry(taskCtx, s)

					if err != nil {
						// Check if writer supports CloseWithError (PipeWriter does)
						if cew, ok := w.(interface{ CloseWithError(error) error }); ok {
							cErr := cew.CloseWithError(err)
							if cErr != nil {
								b.log.ErrorContext(taskCtx, "Error closing segment buffer:", "error", cErr)
							}
						} else if cw, ok := w.(io.Closer); ok {
							_ = cw.Close()
						}
					} else {
						// Close writer on success to signal EOF to readers
						if cw, ok := w.(io.Closer); ok {
							if cErr := cw.Close(); cErr != nil {
								b.log.ErrorContext(taskCtx, "Error closing segment buffer on success:", "error", cErr)
								err = fmt.Errorf("failed to close segment writer: %w", cErr)
							}
						}
					}

					return err
				})
			}

			// Check if all segments are downloaded
			b.mu.Lock()
			allDownloaded := b.nextToDownload >= len(b.rg.segments)

			if len(segmentsToQueue) == 0 && !allDownloaded {
				// Check for cancellation before waiting
				if ctx.Err() != nil {
					b.mu.Unlock()
					break
				}
				// Wait for signal (reader advanced, download finished, or context canceled)
				b.downloadCond.Wait()
				b.mu.Unlock()
				continue
			}
			b.mu.Unlock()

			if allDownloaded {
				break
			}
		}

		// Wait for all downloads to complete
		if err := pool.Wait(); err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return
			}

			// Don't log "closed pipe" errors if we're shutting down or context is canceled
			if strings.Contains(err.Error(), "closed pipe") {
				b.log.DebugContext(ctx, "Suppressed closed pipe error during shutdown", "error", err)
				return
			}

			b.log.DebugContext(ctx, "Error downloading segments:", "error", err)
			return
		}
	case <-ctx.Done():
		return
	}
}
