package vfs

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultReadAheadChunks = 4
	maxConsecutiveErrors   = 10
	circuitBreakerCooldown = 20 * time.Minute
	idleTimeout            = 30 * time.Second
)

// Downloader coordinates background prefetch for a cached file.
// Detects sequential access patterns and prefetches upcoming chunks.
type Downloader struct {
	item      *CacheItem
	opener    FileOpener
	path      string
	fileSize  int64
	chunkSize int64
	readAhead int

	logger *slog.Logger

	// Access pattern tracking
	mu             sync.Mutex
	lastAccessOff  int64
	sequentialHits int
	isSequential   bool

	// Circuit breaker
	consecutiveErrors atomic.Int32
	circuitOpen       atomic.Bool
	circuitOpenedAt   atomic.Int64

	// Prefetch concurrency control
	prefetching atomic.Bool

	// Lifecycle
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopped  atomic.Bool
	lastSeen atomic.Int64
}

// NewDownloader creates a new download coordinator.
func NewDownloader(
	item *CacheItem,
	opener FileOpener,
	path string,
	fileSize int64,
	chunkSize int64,
	readAhead int,
	logger *slog.Logger,
) *Downloader {
	if readAhead <= 0 {
		readAhead = defaultReadAheadChunks
	}

	d := &Downloader{
		item:           item,
		opener:         opener,
		path:           path,
		fileSize:       fileSize,
		chunkSize:      chunkSize,
		readAhead:      readAhead,
		logger:         logger,
		lastAccessOff:  -1,
		sequentialHits: 0,
	}
	d.lastSeen.Store(time.Now().Unix())

	return d
}

// Start begins background monitoring. Call Stop to clean up.
func (d *Downloader) Start(ctx context.Context) {
	ctx, d.cancel = context.WithCancel(ctx)
	d.ctx = ctx

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.idleMonitor(ctx)
	}()
}

// Stop halts the downloader and waits for goroutines to finish.
func (d *Downloader) Stop() {
	if !d.stopped.CompareAndSwap(false, true) {
		return
	}
	if d.cancel != nil {
		d.cancel()
	}
	d.wg.Wait()
}

// RecordAccess records a read access and triggers read-ahead if sequential.
func (d *Downloader) RecordAccess(offset int64) {
	d.lastSeen.Store(time.Now().Unix())

	if d.circuitOpen.Load() {
		// Check cooldown
		openedAt := d.circuitOpenedAt.Load()
		if time.Now().Unix()-openedAt < int64(circuitBreakerCooldown.Seconds()) {
			return
		}
		// Reset circuit breaker
		d.circuitOpen.Store(false)
		d.consecutiveErrors.Store(0)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.lastAccessOff >= 0 {
		delta := offset - d.lastAccessOff
		// Consider sequential if offset advances by up to 2 chunks
		if delta > 0 && delta <= d.chunkSize*2 {
			d.sequentialHits++
			if d.sequentialHits >= 2 {
				d.isSequential = true
			}
		} else {
			d.sequentialHits = 0
			d.isSequential = false
		}
	}

	d.lastAccessOff = offset

	if d.isSequential && !d.stopped.Load() {
		// Only spawn if no prefetch is already running
		if d.prefetching.CompareAndSwap(false, true) {
			d.wg.Add(1)
			go func() {
				defer d.wg.Done()
				defer d.prefetching.Store(false)
				d.prefetch(offset)
			}()
		}
	}
}

// prefetch downloads upcoming chunks starting from the given offset.
func (d *Downloader) prefetch(fromOffset int64) {
	if d.stopped.Load() || d.circuitOpen.Load() {
		return
	}

	// Start prefetching from the next chunk boundary after the current read
	startChunk := (fromOffset / d.chunkSize) + 1

	for i := range d.readAhead {
		if d.stopped.Load() || d.circuitOpen.Load() {
			break
		}

		chunkStart := (startChunk + int64(i)) * d.chunkSize
		chunkEnd := chunkStart + d.chunkSize
		if chunkStart >= d.fileSize {
			break
		}
		if chunkEnd > d.fileSize {
			chunkEnd = d.fileSize
		}

		// Skip if already cached
		if d.item.HasRange(chunkStart, chunkEnd) {
			continue
		}

		if err := d.fetchChunk(chunkStart, chunkEnd); err != nil {
			errCount := d.consecutiveErrors.Add(1)
			if errCount >= maxConsecutiveErrors {
				d.circuitOpen.Store(true)
				d.circuitOpenedAt.Store(time.Now().Unix())
				d.logger.Warn("Downloader circuit breaker opened",
					"path", d.path,
					"errors", errCount)
			}
			return
		}

		d.consecutiveErrors.Store(0)
	}
}

func (d *Downloader) fetchChunk(start, end int64) error {
	file, err := d.opener.Open(d.ctx, d.path)
	if err != nil {
		return err
	}
	defer file.Close()

	rangeLen := end - start
	bufp := fetchBufPool.Get().(*[]byte)
	buf := *bufp
	if int64(cap(buf)) < rangeLen {
		buf = make([]byte, rangeLen)
	} else {
		buf = buf[:rangeLen]
	}

	n, readErr := file.ReadAt(buf, start)
	if n > 0 {
		if _, writeErr := d.item.WriteAt(buf[:n], start); writeErr != nil {
			*bufp = buf
			fetchBufPool.Put(bufp)
			return writeErr
		}
	}

	*bufp = buf
	fetchBufPool.Put(bufp)
	if readErr != nil && readErr != io.EOF {
		return readErr
	}
	return nil
}

// idleMonitor stops the downloader after inactivity.
func (d *Downloader) idleMonitor(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lastSeen := d.lastSeen.Load()
			if time.Now().Unix()-lastSeen > int64(idleTimeout.Seconds()) {
				d.mu.Lock()
				d.isSequential = false
				d.sequentialHits = 0
				d.mu.Unlock()
			}
		}
	}
}
