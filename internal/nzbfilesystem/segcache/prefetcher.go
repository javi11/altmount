package segcache

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// Prefetcher triggers background downloads of upcoming segments when sequential
// access is detected (consecutive segment indices differing by exactly 1).
// It shares the singleflight.Group with SegmentCachedFile to avoid double-fetching.
type Prefetcher struct {
	segments            []SegmentEntry
	cache               *SegmentCache
	opener              FileOpener
	path                string
	readAheadCount      int
	prefetchConcurrency int
	fetchGroup          *singleflight.Group

	mu             sync.Mutex
	lastSegIdx     int
	sequentialHits int
	isSequential   bool

	consecutiveErrors atomic.Int32
	circuitOpen       atomic.Bool

	prefetchCancel context.CancelFunc
	wg             sync.WaitGroup
	stopped        atomic.Bool
	lastSeen       atomic.Int64

	logger *slog.Logger
}

// NewPrefetcher creates a prefetcher that is ready to receive RecordAccess calls.
func NewPrefetcher(
	segments []SegmentEntry,
	cache *SegmentCache,
	opener FileOpener,
	path string,
	readAheadCount, prefetchConcurrency int,
	fetchGroup *singleflight.Group,
	logger *slog.Logger,
) *Prefetcher {
	return &Prefetcher{
		segments:            segments,
		cache:               cache,
		opener:              opener,
		path:                path,
		readAheadCount:      readAheadCount,
		prefetchConcurrency: prefetchConcurrency,
		fetchGroup:          fetchGroup,
		lastSegIdx:          -1,
		logger:              logger,
	}
}

// RecordAccess is called after each ReadAt to update sequential detection state.
// If sequential playback is detected, it launches a background prefetch goroutine.
func (p *Prefetcher) RecordAccess(segIdx int) {
	p.lastSeen.Store(time.Now().UnixNano())

	if p.stopped.Load() || p.circuitOpen.Load() {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.lastSegIdx >= 0 {
		delta := segIdx - p.lastSegIdx
		if delta == 1 {
			p.sequentialHits++
			if p.sequentialHits >= 2 {
				p.isSequential = true
			}
		} else {
			p.sequentialHits = 0
			p.isSequential = false
			if p.prefetchCancel != nil {
				p.prefetchCancel()
				p.prefetchCancel = nil
			}
		}
	}

	p.lastSegIdx = segIdx

	if !p.isSequential {
		return
	}

	// Cancel previous prefetch job and start a new one.
	if p.prefetchCancel != nil {
		p.prefetchCancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.prefetchCancel = cancel
	fromIdx := segIdx

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.prefetchWithCtx(ctx, fromIdx)
	}()
}

func (p *Prefetcher) prefetchWithCtx(ctx context.Context, fromIdx int) {
	end := fromIdx + p.readAheadCount + 1
	if end > len(p.segments) {
		end = len(p.segments)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(p.prefetchConcurrency)

	for i := fromIdx + 1; i < end; i++ {
		if egCtx.Err() != nil {
			break
		}

		seg := p.segments[i]
		if p.cache.Has(seg.MessageID) {
			continue
		}

		eg.Go(func() error {
			if egCtx.Err() != nil {
				return nil
			}

			_, err, _ := p.fetchGroup.Do(seg.MessageID, func() (any, error) {
				if p.cache.Has(seg.MessageID) {
					return nil, nil
				}
				return nil, p.fetchSegment(egCtx, seg)
			})

			if err != nil {
				if p.consecutiveErrors.Add(1) >= 10 {
					p.circuitOpen.Store(true)
					p.logger.WarnContext(egCtx, "segcache: prefetcher circuit breaker opened",
						"path", p.path)
				}
				return nil // Prefetch errors are non-fatal
			}

			p.consecutiveErrors.Store(0)
			return nil
		})
	}

	_ = eg.Wait()
}

func (p *Prefetcher) fetchSegment(ctx context.Context, seg SegmentEntry) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	file, err := p.opener.Open(ctx, p.path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	size := seg.FileEnd - seg.FileStart
	buf := make([]byte, size)

	n, err := file.ReadAt(buf, seg.FileStart)
	if err != nil && err != io.EOF {
		return err
	}

	return p.cache.Put(seg.MessageID, buf[:n])
}

// Stop cancels any running prefetch and waits for goroutines to finish.
func (p *Prefetcher) Stop() {
	p.stopped.Store(true)

	p.mu.Lock()
	if p.prefetchCancel != nil {
		p.prefetchCancel()
		p.prefetchCancel = nil
	}
	p.mu.Unlock()

	p.wg.Wait()
}

// LastSeen returns the time of the most recent RecordAccess call.
func (p *Prefetcher) LastSeen() time.Time {
	return time.Unix(0, p.lastSeen.Load())
}
