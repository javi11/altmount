//go:build linux

package hanwen

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/altmount/internal/fuse/backend"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/spf13/afero"
)

// ensure Handle implements fs.FileReleaser
var _ fs.FileReleaser = (*Handle)(nil)

const (
	opRead = iota
	opSeek
)

type ioReq struct {
	op     int
	dest   []byte
	off    int64
	whence int
	resCh  chan ioResult
}

type ioResult struct {
	n   int
	pos int64
	err error
}

// ioResultChanPool reuses buffered result channels to avoid per-operation allocations.
// Channels are returned to the pool only when the caller successfully receives the result.
// On context cancellation after dispatch, the in-flight channel is not returned to the
// pool; it will be garbage-collected once the worker writes to it.
var ioResultChanPool = sync.Pool{
	New: func() any {
		return make(chan ioResult, 1)
	},
}

// Handle wraps an afero.File for Seek+Read-based reads.
// A single background IO worker goroutine serializes all file operations,
// replacing the previous design that allocated a new goroutine and channel
// on every FUSE read call.
//
// FUSE serializes reads per handle in production; the atomic position tracks
// state for the sequential-read optimization and keeps the race detector happy
// under concurrent tests.
type Handle struct {
	file          afero.File
	closed        atomic.Bool
	logger        *slog.Logger
	path          string
	stream        *nzbfilesystem.ActiveStream
	streamTracker backend.StreamTracker

	// Position tracking for the skip-seek sequential optimization.
	position atomic.Int64

	// Single background IO worker: reqCh feeds requests, wg tracks its lifetime.
	// reqCh is buffered (1) so the caller can dispatch without stalling.
	reqCh chan ioReq
	wg    sync.WaitGroup
}

// NewHandle creates a Handle and starts its background IO worker goroutine.
func NewHandle(
	file afero.File,
	logger *slog.Logger,
	path string,
	stream *nzbfilesystem.ActiveStream,
	st backend.StreamTracker,
) *Handle {
	h := &Handle{
		file:          file,
		logger:        logger,
		path:          path,
		stream:        stream,
		streamTracker: st,
		reqCh:         make(chan ioReq, 1),
	}
	h.wg.Add(1)
	go h.ioWorker()
	return h
}

// ioWorker is the single goroutine that performs all file IO for this handle.
// It exits when reqCh is closed (triggered by Release).
func (h *Handle) ioWorker() {
	defer h.wg.Done()
	for req := range h.reqCh {
		var res ioResult
		switch req.op {
		case opRead:
			res.n, res.err = h.file.Read(req.dest)
		case opSeek:
			res.pos, res.err = h.file.Seek(req.off, req.whence)
		}
		req.resCh <- res
	}
}

// execIO dispatches an IO request to the worker and waits for the result.
// Returns (ioResult{}, ctx.Err()) on context cancellation.
// If ctx fires after the request has already been dispatched, the in-flight
// result channel is abandoned (not pooled) and GC'd once the worker writes.
func (h *Handle) execIO(ctx context.Context, req ioReq) (ioResult, error) {
	resCh := ioResultChanPool.Get().(chan ioResult)
	req.resCh = resCh

	select {
	case h.reqCh <- req:
	case <-ctx.Done():
		ioResultChanPool.Put(resCh)
		return ioResult{}, ctx.Err()
	}

	select {
	case res := <-resCh:
		ioResultChanPool.Put(resCh)
		return res, nil
	case <-ctx.Done():
		// Worker will write to resCh; do NOT return to pool.
		return ioResult{}, ctx.Err()
	}
}

// Read handles a FUSE read request using the persistent IO worker.
// Non-sequential reads call Seek on the underlying file; MetadataVirtualFile.Seek
// handles the drain-forward optimization transparently for small gaps.
func (h *Handle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if h.closed.Load() {
		return nil, syscall.EIO
	}

	curPos := h.position.Load()
	if off != curPos {
		res, err := h.execIO(ctx, ioReq{op: opSeek, off: off, whence: io.SeekStart})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				h.logger.DebugContext(ctx, "Seek canceled", "path", h.path, "offset", off)
				return nil, syscall.EINTR
			}
			h.logger.ErrorContext(ctx, "Seek failed", "path", h.path, "offset", off, "error", err)
			return nil, syscall.EIO
		}
		if res.err != nil {
			h.logger.ErrorContext(ctx, "Seek failed", "path", h.path, "offset", off, "error", res.err)
			return nil, syscall.EIO
		}
	}

	res, err := h.execIO(ctx, ioReq{op: opRead, dest: dest})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			h.logger.DebugContext(ctx, "Read canceled", "path", h.path, "offset", off)
			return nil, syscall.EINTR
		}
		h.logger.ErrorContext(ctx, "Read failed", "path", h.path, "offset", off, "size", len(dest), "error", err)
		return nil, syscall.EIO
	}

	n := res.n
	if n > 0 {
		newPos := off + int64(n)
		h.position.Store(newPos)
		if h.stream != nil {
			h.streamTracker.UpdateProgress(h.stream.ID, int64(n))
			atomic.StoreInt64(&h.stream.CurrentOffset, newPos)
		}
	}

	if res.err != nil && res.err != io.EOF {
		h.logger.ErrorContext(ctx, "Read failed", "path", h.path, "offset", off, "size", len(dest), "error", res.err)
		return nil, syscall.EIO
	}

	return fuse.ReadResultData(dest[:n]), 0
}

// Flush is a no-op (read-only filesystem).
func (h *Handle) Flush(ctx context.Context) syscall.Errno {
	return 0
}

// Fsync is a no-op (read-only filesystem).
func (h *Handle) Fsync(ctx context.Context, flags uint32) syscall.Errno {
	return 0
}

// Release closes the file and stops the background IO worker.
// It is idempotent.
func (h *Handle) Release(ctx context.Context) syscall.Errno {
	if !h.closed.CompareAndSwap(false, true) {
		return 0
	}

	if h.stream != nil && h.streamTracker != nil {
		h.streamTracker.Remove(h.stream.ID)
		h.stream = nil
	}

	// Close the file first so any in-progress blocking IO in the worker fails fast.
	if h.file != nil {
		if err := h.file.Close(); err != nil {
			h.logger.ErrorContext(ctx, "Close failed", "path", h.path, "error", err)
		}
	}

	// Signal the worker to exit, then wait for it to finish.
	close(h.reqCh)
	h.wg.Wait()

	return 0
}
