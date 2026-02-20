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
	"github.com/javi11/nntppool/v4"
)

const (
	defaultMaxPrefetch = 30 // Default to 30 segments prefetched ahead
)

var (
	_ io.ReadCloser = &UsenetReader{}
)

type MetricsTracker interface {
	IncArticlesDownloaded()
	IncArticlesPosted()
	UpdateDownloadProgress(id string, bytesDownloaded int64)
}

// SegmentStore is an optional cache for decoded segment data.
// Implementations must be safe for concurrent use.
type SegmentStore interface {
	Get(messageID string) ([]byte, bool)
	Put(messageID string, data []byte) error
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
	log            *slog.Logger
	wg             sync.WaitGroup
	ctx            context.Context // Reader's context for cancellation
	cancel         context.CancelFunc
	rg             *segmentRange
	maxPrefetch    int // Maximum segments prefetched ahead of current read position
	init           chan any
	initDownload   sync.Once
	closeOnce      sync.Once
	totalBytesRead int64
	poolGetter     func() (*nntppool.Client, error) // Dynamic pool getter
	metricsTracker MetricsTracker
	streamID       string
	segmentStore   SegmentStore // optional, nil = no caching

	// Prefetch-based download tracking
	nextToDownload int // Index of next segment to schedule

	mu sync.Mutex
}

func NewUsenetReader(
	ctx context.Context,
	poolGetter func() (*nntppool.Client, error),
	rg *segmentRange,
	maxPrefetch int,
	metricsTracker MetricsTracker,
	streamID string,
	segmentStore SegmentStore,
) (*UsenetReader, error) {
	log := slog.Default().With("component", "usenet-reader")
	ctx, cancel := context.WithCancel(ctx)

	if maxPrefetch <= 0 {
		maxPrefetch = defaultMaxPrefetch
	}

	ur := &UsenetReader{
		log:            log,
		ctx:            ctx,
		cancel:         cancel,
		rg:             rg,
		init:           make(chan any, 1),
		maxPrefetch:    maxPrefetch,
		poolGetter:     poolGetter,
		metricsTracker: metricsTracker,
		streamID:       streamID,
		segmentStore:   segmentStore,
	}

	ur.wg.Go(func() {
		ur.downloadManager(ctx)
	})

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

		// Unblock any pending reads waiting for data
		if b.rg != nil {
			b.rg.CloseSegments()
		}

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
		nn, err := s.GetReaderContext(b.ctx).Read(p[n:])
		n += nn

		b.mu.Lock()
		b.totalBytesRead += int64(nn)
		totalRead := b.totalBytesRead
		b.mu.Unlock()

		if err != nil {
			if errors.Is(err, io.EOF) {
				// Segment fully read — move to next segment
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

// downloadSegmentWithRetry attempts to download a segment with retry logic for pool unavailability
func (b *UsenetReader) downloadSegmentWithRetry(ctx context.Context, seg *segment) ([]byte, error) {
	// Cache HIT: skip NNTP entirely
	if b.segmentStore != nil {
		if data, ok := b.segmentStore.Get(seg.Id); ok {
			if b.metricsTracker != nil {
				b.metricsTracker.IncArticlesDownloaded()
				if b.streamID != "" {
					b.metricsTracker.UpdateDownloadProgress(b.streamID, int64(len(data)))
				}
			}
			return data, nil
		}
	}

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

			if b.metricsTracker != nil {
				b.metricsTracker.IncArticlesDownloaded()

				if b.streamID != "" {
					b.metricsTracker.UpdateDownloadProgress(b.streamID, int64(len(resultBytes)))
				}
			}

			return nil
		},
		retry.Attempts(10),
		retry.Delay(50*time.Millisecond),
		retry.MaxDelay(2*time.Second),
		retry.DelayType(retry.BackOffDelay),
		retry.OnRetry(func(n uint, err error) {
			b.log.DebugContext(ctx, "Pool unavailable or timeout, retrying segment download",
				"attempt", n+1,
				"segment_id", seg.Id,
				"error", err)
		}),
		retry.Context(ctx),
	)

	// Cache WRITE: tee-write after successful download (fire-and-forget)
	if b.segmentStore != nil && resultBytes != nil && err == nil {
		_ = b.segmentStore.Put(seg.Id, resultBytes)
	}

	return resultBytes, err
}

func (b *UsenetReader) downloadManager(ctx context.Context) {
	select {
	case _, ok := <-b.init:
		if !ok {
			return
		}
	case <-ctx.Done():
		return
	}

	if b.rg.Len() == 0 {
		return
	}

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

		// Limit how far ahead we prefetch beyond the current read position
		currentRead := b.rg.GetCurrentIndex()
		if b.nextToDownload-currentRead >= b.maxPrefetch {
			b.mu.Unlock()
			// Wait briefly before re-checking
			select {
			case <-time.After(50 * time.Millisecond):
				continue
			case <-ctx.Done():
				return
			}
		}

		// Schedule next segment for download
		idx := b.nextToDownload
		b.nextToDownload++
		b.mu.Unlock()

		seg, err := b.rg.GetSegment(idx)
		if err != nil || seg == nil {
			continue
		}

		go func(segIdx int, s *segment) {
			defer func() {
				if p := recover(); p != nil {
					b.log.ErrorContext(ctx, "Panic in download task:", "panic", p)
					s.SetError(fmt.Errorf("panic in download task: %v", p))
				}
			}()

			taskCtx := slogutil.With(ctx, "segment_id", s.Id, "segment_idx", segIdx)
			data, err := b.downloadSegmentWithRetry(taskCtx, s)

			if err != nil {
				s.SetError(err)
			} else {
				s.SetData(data)
			}
		}(idx, seg)
	}
}
