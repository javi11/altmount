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

// readAtContexter is implemented by virtual files that honor per-read cancellation
// (e.g. MetadataVirtualFile). Checked before io.ReaderAt.
type readAtContexter interface {
	ReadAtContext(ctx context.Context, p []byte, off int64) (n int, err error)
}

// Handle wraps an afero.File for FUSE reads.
// When useReadAt is true and the file implements ReadAtContext or io.ReaderAt,
// reads use offset-native APIs on the calling goroutine (no Seek cursor churn).
// Otherwise a background IO worker serializes Seek+Read.
//
// The atomic position tracks the last completed read end offset for logging and
// stream tracking; it does not gate correctness when using ReadAt.
type Handle struct {
	file          afero.File
	useReadAt     bool
	closed        atomic.Bool
	logger        *slog.Logger
	path          string
	stream        *nzbfilesystem.ActiveStream
	streamTracker backend.StreamTracker

	position atomic.Int64

	// readAtMu serializes offset-native reads per handle so MetadataVirtualFile can
	// safely reuse a shared streaming reader without concurrent ReadAt corruption.
	readAtMu sync.Mutex

	reqCh chan ioReq
	wg    sync.WaitGroup
}

// NewHandle creates a Handle and starts its background IO worker goroutine.
// useReadAt selects the offset-native read path when the file supports it
// (see config fuse.use_read_at).
func NewHandle(
	file afero.File,
	logger *slog.Logger,
	path string,
	stream *nzbfilesystem.ActiveStream,
	st backend.StreamTracker,
	useReadAt bool,
) *Handle {
	h := &Handle{
		file:          file,
		useReadAt:     useReadAt,
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

func (h *Handle) applyReadResult(off int64, n int, readErr error) syscall.Errno {
	if n > 0 {
		newPos := off + int64(n)
		h.position.Store(newPos)
		if h.stream != nil && h.streamTracker != nil {
			h.streamTracker.UpdateProgress(h.stream.ID, int64(n))
			atomic.StoreInt64(&h.stream.CurrentOffset, newPos)
		}
	}
	if readErr != nil && readErr != io.EOF {
		return syscall.EIO
	}
	return 0
}

// Read handles a FUSE read request.
func (h *Handle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if h.closed.Load() {
		return nil, syscall.EIO
	}

	if h.useReadAt {
		if rac, ok := h.file.(readAtContexter); ok {
			h.readAtMu.Lock()
			defer h.readAtMu.Unlock()
			n, err := rac.ReadAtContext(ctx, dest, off)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					h.logger.DebugContext(ctx, "ReadAtContext canceled", "path", h.path, "offset", off)
					return nil, syscall.EINTR
				}
				if err != io.EOF {
					h.logger.ErrorContext(ctx, "ReadAtContext failed", "path", h.path, "offset", off, "size", len(dest), "error", err)
					return nil, syscall.EIO
				}
			}
			if errno := h.applyReadResult(off, n, err); errno != 0 {
				return nil, errno
			}
			return fuse.ReadResultData(dest[:n]), 0
		}
		if ra, ok := h.file.(io.ReaderAt); ok {
			h.readAtMu.Lock()
			defer h.readAtMu.Unlock()
			n, err := ra.ReadAt(dest, off)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					h.logger.DebugContext(ctx, "ReadAt canceled", "path", h.path, "offset", off)
					return nil, syscall.EINTR
				}
				if err != io.EOF {
					h.logger.ErrorContext(ctx, "ReadAt failed", "path", h.path, "offset", off, "size", len(dest), "error", err)
					return nil, syscall.EIO
				}
			}
			if errno := h.applyReadResult(off, n, err); errno != 0 {
				return nil, errno
			}
			return fuse.ReadResultData(dest[:n]), 0
		}
	}

	// Skip seek when already at the correct position (sequential-read optimisation).
	if off != h.position.Load() {
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
	if errno := h.applyReadResult(off, n, res.err); errno != 0 {
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
