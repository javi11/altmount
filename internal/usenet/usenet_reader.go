package usenet

import (
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
	"github.com/javi11/altmount/internal/holes"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/slogutil"
	"github.com/javi11/nntppool/v4"
)

const (
	defaultMaxPrefetch = 60 // Default to 60 segments prefetched ahead
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

// HoleHooks lets the owner of a reader decide, synchronously, what happens
// when a segment is confirmed missing (ErrArticleNotFound — never retried).
// The reader stays dumb: it asks, the owner accounts, persists and
// transitions health status. Segments approved for padding are zero-filled
// in place so the read loop never sees an error and playback continues.
// Both callbacks run on download goroutines: they must be concurrency-safe
// and fast (no network).
type HoleHooks struct {
	// OnHole returns the pad/fail decision for a missing segment, identified
	// by its index in the file's segment space.
	OnHole func(segIndex int, segID string) holes.Decision
	// KnownHoles reports segments already known missing: those are
	// zero-filled immediately, without any fetch (replay pre-pad).
	KnownHoles func(segIndex int) bool
}

// ReaderOption customizes a UsenetReader.
type ReaderOption func(*UsenetReader)

// WithHoleHooks enables zero-filling of confirmed-missing segments under the
// owner's control. Without it, a missing segment fails the read as always.
func WithHoleHooks(h *HoleHooks) ReaderOption {
	return func(r *UsenetReader) {
		r.holeHooks = h
	}
}

type DataCorruptionError struct {
	UnderlyingErr error
	BytesRead     int64
	NoRetry       bool
	// FileOffset is the absolute file-coordinate position where the failure
	// surfaced (-1 when unknown), enabling playback-impact classification.
	FileOffset int64
	// SegmentID is the message ID of the failing segment, when known.
	SegmentID string
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
	poolGetter     func() (pool.NntpClient, error) // Dynamic pool getter
	metricsTracker MetricsTracker
	streamID       string
	segmentStore   SegmentStore // optional, nil = no caching
	holeHooks      *HoleHooks   // optional, nil = missing segments fail the read
	cond           *sync.Cond   // Signals downloadManager when reader advances

	// Prefetch-based download tracking
	nextToDownload int // Index of next segment to schedule

	// Tracing counters (atomic, no lock needed)
	inFlight atomic.Int32 // goroutines actively downloading right now

	mu sync.Mutex
}

func NewUsenetReader(
	ctx context.Context,
	poolGetter func() (pool.NntpClient, error),
	rg *segmentRange,
	maxPrefetch int,
	metricsTracker MetricsTracker,
	streamID string,
	segmentStore SegmentStore,
	opts ...ReaderOption,
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
	for _, opt := range opts {
		opt(ur)
	}

	ur.cond = sync.NewCond(&ur.mu)

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

// Interrupt cancels the reader's context and signals any blocked Read
// to return. Non-blocking and idempotent; safe to call concurrently
// with Read or Close. The caller is still responsible for invoking
// Close to release goroutines and resources. Used by callers (like
// MetadataVirtualFile.Close) that need to abort an in-flight download
// without taking the file's own lock.
func (b *UsenetReader) Interrupt() {
	b.cancel()
	b.cond.Broadcast()
	b.mu.Lock()
	rg := b.rg
	b.mu.Unlock()
	if rg != nil {
		rg.CloseSegments()
	}
}

func (b *UsenetReader) Close() error {
	b.closeOnce.Do(func() {
		b.cancel()

		// Unblock downloadManager if it's waiting on the cond
		b.cond.Broadcast()

		// Unblock any pending reads waiting for data
		if b.rg != nil {
			b.rg.CloseSegments()
		}

		// Wait for goroutines with timeout. The cancel() above ensures all
		// goroutines will eventually terminate, so the waiter goroutine is
		// not a permanent leak — it cleans up once downloads finish.
		// A periodic Broadcast pokes goroutines that entered cond.Wait()
		// after the initial Broadcast above.
		done := make(chan struct{})
		go func() {
			b.wg.Wait()
			close(done)
		}()

		deadline := time.NewTimer(30 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

	loop:
		for {
			select {
			case <-done:
				break loop
			case <-deadline.C:
				b.log.WarnContext(b.ctx, "Timeout waiting for downloads to complete during close")
				break loop
			case <-ticker.C:
				b.cond.Broadcast()
			}
		}

		b.mu.Lock()
		if b.rg != nil {
			_ = b.rg.Clear()
			b.rg = nil
		}
		b.mu.Unlock()

		// Final wake for any goroutines that entered cond.Wait() after the loop
		b.cond.Broadcast()
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
					FileOffset:    rg.start + totalRead,
				}
			} else {
				return 0, &DataCorruptionError{
					UnderlyingErr: err,
					BytesRead:     0,
					FileOffset:    rg.start,
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
				if err == nil {
					// Wake download manager — room for more prefetch
					b.cond.Signal()
				}

				if err != nil {
					if n > 0 {
						return n, nil
					}

					if b.isArticleNotFoundError(err) {
						if totalRead > 0 {
							return n, &DataCorruptionError{
								UnderlyingErr: err,
								BytesRead:     totalRead,
								FileOffset:    rg.start + totalRead,
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
						FileOffset:    rg.start + totalRead,
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

// IsArticleNotFound reports whether err stems from an article missing on all
// providers (permanent, never retried) — the only failure the hole model
// treats as a hole.
func IsArticleNotFound(err error) bool {
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
			b.log.DebugContext(ctx, "segment cache hit",
				"segment_id", seg.Id,
				"size_bytes", len(data),
			)
			return data, nil
		}
	}

	// Fix B: hoist pool getter outside retry loop — pool errors are not retriable
	// per-download-attempt; if the pool is unavailable we fail fast.
	poolGetStart := time.Now()
	cp, poolErr := b.poolGetter()
	poolGetDur := time.Since(poolGetStart)
	if poolErr != nil {
		b.log.DebugContext(ctx, "pool get failed",
			"segment_id", seg.Id,
			"pool_get_dur", poolGetDur,
			"error", poolErr,
		)
		return nil, poolErr
	}
	if poolGetDur > 100*time.Millisecond {
		b.log.DebugContext(ctx, "slow pool get",
			"segment_id", seg.Id,
			"pool_get_dur", poolGetDur,
		)
	}

	segStart := time.Now()
	var resultBytes []byte
	err := retry.Do(
		func() error {
			// Fix C: reduce per-attempt timeout 30s → 15s to free stuck connections faster
			attemptCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()

			fetchStart := time.Now()
			result, err := cp.BodyPriority(attemptCtx, seg.Id)
			fetchDur := time.Since(fetchStart)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					b.log.DebugContext(ctx, "segment download timed out after 15s",
						"segment_id", seg.Id,
						"fetch_dur", fetchDur,
					)
				}

				var bytesWritten int64
				if result != nil {
					bytesWritten = int64(result.BytesDecoded)
				}

				if strings.Contains(err.Error(), "data corruption detected") {
					return &DataCorruptionError{
						UnderlyingErr: err,
						BytesRead:     bytesWritten,
						FileOffset:    -1,
						SegmentID:     seg.Id,
					}
				}

				return err
			}

			resultBytes = result.Bytes
			b.metricsTracker.IncArticlesDownloaded()
			b.metricsTracker.UpdateDownloadProgress(b.streamID, int64(len(resultBytes)))

			return nil
		},
		// Retry strategy (post-S1/S3 fix):
		// - ErrArticleNotFound: never retry (article is permanently gone).
		// - DeadlineExceeded: retry immediately, no backoff — a fresh
		//   nntppool connection is available via round-robin.
		// - Other errors: at most one retry (Attempts=2 total wire calls
		//   per failure), with exponential backoff + jitter to break
		//   thundering-herd synchronization across readers. Base=50ms,
		//   max jitter=100ms → first retry delay drawn from [50, 150]ms.
		retry.Attempts(2),
		retry.Delay(50*time.Millisecond),
		retry.MaxJitter(100*time.Millisecond),
		retry.DelayType(func(n uint, err error, config *retry.Config) time.Duration {
			if errors.Is(err, context.DeadlineExceeded) {
				return 0
			}
			return retry.CombineDelay(retry.BackOffDelay, retry.RandomDelay)(n, err, config)
		}),
		retry.RetryIf(func(err error) bool {
			if errors.Is(err, nntppool.ErrArticleNotFound) {
				return false // permanent failure — do not retry
			}
			return true
		}),
		retry.OnRetry(func(n uint, err error) {
			if !errors.Is(err, context.Canceled) && ctx.Err() == nil {
				b.log.DebugContext(ctx, "segment download retry",
					"attempt", n+1,
					"segment_id", seg.Id,
					"error", err,
					"elapsed", time.Since(segStart),
				)
			}
		}),
		retry.Context(ctx),
	)

	// Cache WRITE: tee-write after successful download (fire-and-forget)
	if b.segmentStore != nil && resultBytes != nil && err == nil {
		_ = b.segmentStore.Put(seg.Id, resultBytes)
	}

	if errors.Is(err, nntppool.ErrArticleNotFound) {
		b.log.DebugContext(ctx, "missing segment",
			"segment_id", seg.Id,
		)
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

	totalSegments := b.rg.Len()

	for ctx.Err() == nil {
		b.mu.Lock()
		if b.rg == nil {
			b.mu.Unlock()
			return
		}

		// Check if all segments have been scheduled
		if b.nextToDownload >= totalSegments {
			b.mu.Unlock()
			break
		}

		// Limit how far ahead we prefetch beyond the current read position
		currentRead := b.rg.GetCurrentIndex()
		ahead := b.nextToDownload - currentRead
		if ahead >= b.maxPrefetch {
			b.cond.Wait()
			b.mu.Unlock()
			if ctx.Err() != nil {
				return
			}
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

		b.inFlight.Add(1)
		go func(segIdx int, s *segment) {
			defer b.inFlight.Add(-1)
			defer b.cond.Signal()
			defer func() {
				if p := recover(); p != nil {
					b.log.ErrorContext(ctx, "Panic in download task:", "panic", p)
					s.SetError(fmt.Errorf("panic in download task: %v", p))
				}
			}()

			taskCtx := slogutil.With(ctx, "segment_id", s.Id, "segment_idx", segIdx)

			// Replay pre-pad: a segment already known missing (persisted hole
			// map) zero-fills immediately, with no fetch round-trip.
			if b.holeHooks != nil && b.holeHooks.KnownHoles != nil && b.holeHooks.KnownHoles(s.loaderIdx) {
				b.log.DebugContext(taskCtx, "zero-filling known-missing segment without fetch")
				s.SetData(make([]byte, s.End+1))
				return
			}

			data, err := b.downloadSegmentWithRetry(taskCtx, s)

			if err != nil {
				// A confirmed-missing article may be zero-filled instead of
				// failing the stream, when the owner's hole hook approves.
				if b.holeHooks != nil && b.holeHooks.OnHole != nil &&
					errors.Is(err, nntppool.ErrArticleNotFound) &&
					b.holeHooks.OnHole(s.loaderIdx, s.Id) == holes.DecisionPad {
					b.log.InfoContext(taskCtx, "zero-filling missing segment",
						"file_segment_index", s.loaderIdx)
					s.SetData(make([]byte, s.End+1))
					return
				}
				s.SetError(err)
			} else {
				s.SetData(data)
			}
		}(idx, seg)
	}

}
