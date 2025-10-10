package usenet

import (
	"context"
	"errors"
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
	_ io.ReadCloser = &usenetReader{}
)

type ArticleNotFoundError struct {
	UnderlyingErr error
	BytesRead     int64
}

func (e *ArticleNotFoundError) Error() string {
	return e.UnderlyingErr.Error()
}

func (e *ArticleNotFoundError) Unwrap() error {
	return e.UnderlyingErr
}

type usenetReader struct {
	log                *slog.Logger
	wg                 sync.WaitGroup
	cancel             context.CancelFunc
	rg                 segmentRange
	maxDownloadWorkers int
	maxCacheSize       int64 // Maximum cache size in bytes
	init               chan any
	initDownload       sync.Once
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
	rg segmentRange,
	maxDownloadWorkers int,
	maxCacheSizeMB int,
) (io.ReadCloser, error) {
	log := slog.Default()
	ctx, cancel := context.WithCancel(ctx)

	// Convert MB to bytes
	maxCacheSize := int64(maxCacheSizeMB) * 1024 * 1024
	if maxCacheSize <= 0 {
		maxCacheSize = defaultMaxCacheSize
	}

	ur := &usenetReader{
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

func (b *usenetReader) Close() error {
	b.cancel()
	close(b.init)

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
		_ = b.rg.Clear()
		b.rg = segmentRange{}
	case <-time.After(30 * time.Second):
		// Timeout waiting for downloads to complete
		// This prevents hanging but logs the issue
		b.log.Warn("Timeout waiting for downloads to complete during close, potential goroutine leak")
		// Still attempt to clear resources
		_ = b.rg.Clear()
		b.rg = segmentRange{}
	}

	return nil
}

// Read reads len(p) byte from the Buffer starting at the current offset.
// It returns the number of bytes read and an error if any.
// Returns io.EOF error if pointer is at the end of the Buffer.
func (b *usenetReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	b.initDownload.Do(func() {
		b.init <- struct{}{}
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
				return 0, &ArticleNotFoundError{
					UnderlyingErr: err,
					BytesRead:     totalRead,
				}
			} else {
				// No data read at all - this is corrupted/missing
				return 0, &ArticleNotFoundError{
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
							return n, &ArticleNotFoundError{
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
					return n, &ArticleNotFoundError{
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
func (b *usenetReader) isArticleNotFoundError(err error) bool {
	return errors.Is(err, nntppool.ErrArticleNotFoundInProviders)
}

// isPoolUnavailableError checks if the error indicates the pool is unavailable or shutdown
func (b *usenetReader) isPoolUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection pool is shutdown") ||
		strings.Contains(errStr, "connection pool not available") ||
		strings.Contains(errStr, "NNTP connection pool not available")
}

// downloadSegmentWithRetry attempts to download a segment with retry logic for pool unavailability
func (b *usenetReader) downloadSegmentWithRetry(ctx context.Context, segmentID string, writer io.Writer, groups []string) error {
	return retry.Do(
		func() error {
			// Get current pool
			cp, err := b.poolGetter()
			if err != nil {
				return err
			}

			// Attempt download
			_, err = cp.Body(ctx, segmentID, writer, groups)
			return err
		},
		retry.Attempts(10),
		retry.Delay(50*time.Millisecond),
		retry.MaxDelay(2*time.Second),
		retry.DelayType(retry.BackOffDelay),
		retry.RetryIf(func(err error) bool {
			// Only retry if error is pool-related
			return b.isPoolUnavailableError(err)
		}),
		retry.OnRetry(func(n uint, err error) {
			b.log.DebugContext(ctx, "Pool unavailable, retrying segment download",
				"attempt", n+1,
				"segment_id", segmentID,
				"error", err)
		}),
		retry.Context(ctx),
	)
}

func (b *usenetReader) downloadManager(
	ctx context.Context,
) {
	select {
	case _, ok := <-b.init:
		if !ok {
			return
		}

		downloadWorkers := b.maxDownloadWorkers
		if downloadWorkers == 0 {
			downloadWorkers = defaultDownloadWorkers
		}

		if len(b.rg.segments) == 0 {
			b.log.DebugContext(ctx, "No segments to download")

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
			// Get current reading position
			currentIndex := b.rg.GetCurrentIndex()

			// Calculate how many segments we should have downloaded
			targetDownload := currentIndex + maxSegmentsAhead
			if targetDownload > len(b.rg.segments) {
				targetDownload = len(b.rg.segments)
			}

			// Download segments that are not yet downloaded or downloading
			b.mu.Lock()
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
			b.mu.Unlock()

			// Queue downloads for new segments
			for _, idx := range segmentsToQueue {
				if ctx.Err() != nil {
					break
				}

				segmentIdx := idx // Capture for closure
				s := b.rg.segments[segmentIdx]

				pool.Go(func(c context.Context) error {
					w := s.writer

					// Set the item ready to read
					ctx = slogutil.With(ctx, "segment_id", s.Id, "segment_idx", segmentIdx)
					err := b.downloadSegmentWithRetry(ctx, s.Id, s.Writer(), s.groups)

					// Mark download complete
					b.mu.Lock()
					if segmentIdx >= b.nextToDownload {
						b.nextToDownload = segmentIdx + 1
					}
					delete(b.downloadingSegments, segmentIdx)
					b.downloadCond.Signal()
					b.mu.Unlock()

					if !errors.Is(err, context.Canceled) {
						cErr := w.CloseWithError(err)
						if cErr != nil {
							b.log.ErrorContext(ctx, "Error closing segment buffer:", "error", cErr)
						}

						if err != nil && !errors.Is(err, context.Canceled) {
							b.log.DebugContext(ctx, "Error downloading segment:", "error", err)
							return err
						}

						return nil
					}

					err = w.Close()
					if err != nil {
						b.log.ErrorContext(ctx, "Error closing segment writer:", "error", err)
					}

					return nil
				})
			}

			// Check if all segments are downloaded
			b.mu.Lock()
			allDownloaded := b.nextToDownload >= len(b.rg.segments)
			b.mu.Unlock()

			if allDownloaded {
				break
			}

			// Wait a bit before checking again to avoid busy-waiting
			select {
			case <-time.After(100 * time.Millisecond):
				continue
			case <-ctx.Done():
				// Context is done, next iteration will break the loop
				continue
			}
		}

		// Wait for all downloads to complete
		if err := pool.Wait(); err != nil {
			b.log.DebugContext(ctx, "Error downloading segments:", "error", err)
			return
		}
	case <-ctx.Done():
		return
	}
}
