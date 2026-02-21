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

// bufPool reuses download buffers to reduce GC pressure.
// Pre-sized for typical Usenet segments (~750KB decoded).
var bufPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 750*1024))
	},
}

type UsenetReader struct {
	log          *slog.Logger
	wg           sync.WaitGroup
	ctx          context.Context // Reader's context for cancellation
	cancel       context.CancelFunc
	rg           *segmentRange
	maxPrefetch  int // Maximum segments prefetched ahead of current read position
	init         chan any
	initDownload sync.Once
	closeOnce    sync.Once
	poolGetter   func() (*nntppool.Client, error) // Dynamic pool getter
	metricsTracker MetricsTracker
	streamID       string
	segmentStore   SegmentStore // optional, nil = no caching

	// Prefetch-based download tracking (atomic: written by downloadManager, read from multiple goroutines)
	nextToDownload  atomic.Int64
	totalBytesRead  atomic.Int64

	// segmentConsumed is signaled (non-blocking, capacity 1) whenever the reader
	// consumes a segment via Next(), allowing downloadManager to immediately
	// schedule the next download instead of waiting for the 50ms poll timer.
	segmentConsumed chan struct{}

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
		log:             log,
		ctx:             ctx,
		cancel:          cancel,
		rg:              rg,
		init:            make(chan any, 1),
		maxPrefetch:     maxPrefetch,
		poolGetter:      poolGetter,
		metricsTracker:  metricsTracker,
		streamID:        streamID,
		segmentStore:    segmentStore,
		segmentConsumed: make(chan struct{}, 1),
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
		totalRead := b.totalBytesRead.Load()

		if b.isArticleNotFoundError(err) {
			return 0, &DataCorruptionError{
				UnderlyingErr: err,
				BytesRead:     totalRead,
			}
		}
		return 0, io.EOF
	}

	n := 0
	for n < len(p) {
		nn, err := s.GetReaderContext(b.ctx).Read(p[n:])
		n += nn

		totalRead := b.totalBytesRead.Add(int64(nn))

		if err != nil {
			if errors.Is(err, io.EOF) {
				// Segment fully read — signal download manager to schedule the next
				// segment immediately rather than waiting for the polling interval.
				select {
				case b.segmentConsumed <- struct{}{}:
				default:
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
	rg := b.rg
	b.mu.Unlock()

	if rg == nil {
		return 0
	}

	nextToDownload := int(b.nextToDownload.Load())
	if nextToDownload == 0 {
		return 0
	}

	idx := nextToDownload - 1
	s, err := rg.GetSegment(idx)
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

			// Reuse buffer from pool to reduce GC pressure.
			buf := bufPool.Get().(*bytes.Buffer)
			buf.Reset()

			result, err := cp.BodyStream(attemptCtx, seg.Id, buf)
			if err != nil {
				bufPool.Put(buf)

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

			// Copy decoded data out before returning buffer to pool.
			resultBytes = make([]byte, buf.Len())
			copy(resultBytes, buf.Bytes())
			bufPool.Put(buf)

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

	// Cache the rg reference once. Safe because this goroutine is tracked in b.wg,
	// and rg.Clear() is only called after wg.Wait() completes in Close().
	b.mu.Lock()
	rg := b.rg
	b.mu.Unlock()

	if rg == nil || rg.Len() == 0 {
		return
	}

	// Cache total segment count — fixed after segmentRange is built.
	totalSegments := rg.Len()

	for ctx.Err() == nil {
		nextToDownload := int(b.nextToDownload.Load())

		// All segments have been scheduled — exit the loop.
		if nextToDownload >= totalSegments {
			break
		}

		// GetCurrentIndex is now lock-free (atomic read).
		// Limit how far ahead we prefetch beyond the current read position.
		currentRead := rg.GetCurrentIndex()
		if nextToDownload-currentRead >= b.maxPrefetch {
			// Block until the reader consumes a segment (immediate wake-up)
			// or the fallback 1s timeout fires (in case the signal was missed).
			select {
			case <-b.segmentConsumed:
			case <-time.After(1 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}

		seg, err := rg.GetSegment(nextToDownload)
		if err != nil || seg == nil {
			b.nextToDownload.Add(1)
			continue
		}

		idx := nextToDownload
		b.nextToDownload.Add(1)

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
