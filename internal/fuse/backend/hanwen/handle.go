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

// Handle wraps an afero.File for Seek+Read-based reads.
// Uses mutex-protected Seek+Read to preserve the persistent reader's
// prefetch state in UsenetReader (ReadAt creates a new reader per call).
type Handle struct {
	file          afero.File
	closed        atomic.Bool
	logger        *slog.Logger
	path          string
	stream        *nzbfilesystem.ActiveStream
	streamTracker backend.StreamTracker

	// Seek+Read serialization
	mu       sync.Mutex
	position int64
}

// NewHandle creates a new Handle for Seek+Read based access.
func NewHandle(
	file afero.File,
	logger *slog.Logger,
	path string,
	stream *nzbfilesystem.ActiveStream,
	st backend.StreamTracker,
) *Handle {
	return &Handle{
		file:          file,
		logger:        logger,
		path:          path,
		stream:        stream,
		streamTracker: st,
	}
}

// Read handles a read request using mutex-protected Seek+Read.
// This keeps the persistent UsenetReader alive across reads, allowing
// the downloadManager prefetch pipeline to stay effective.
func (h *Handle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if h.closed.Load() {
		return nil, syscall.EIO
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Skip seek if already at the correct position (sequential read optimization)
	if off != h.position {
		if _, err := h.file.Seek(off, io.SeekStart); err != nil {
			h.logger.ErrorContext(ctx, "Seek failed", "path", h.path, "offset", off, "error", err)
			return nil, syscall.EIO
		}
	}

	n, err := h.file.Read(dest)

	if n > 0 {
		h.position = off + int64(n)
		if h.stream != nil {
			h.streamTracker.UpdateProgress(h.stream.ID, int64(n))
			atomic.StoreInt64(&h.stream.CurrentOffset, h.position)
		}
	}

	if err != nil && err != io.EOF {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			h.logger.DebugContext(ctx, "Read canceled", "path", h.path, "offset", off)
			return nil, syscall.EINTR
		}

		h.logger.ErrorContext(ctx, "Read failed", "path", h.path, "offset", off, "size", len(dest), "error", err)
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

// Release closes the file when the handle is released.
func (h *Handle) Release(ctx context.Context) syscall.Errno {
	if !h.closed.CompareAndSwap(false, true) {
		return 0
	}

	if h.stream != nil && h.streamTracker != nil {
		h.streamTracker.Remove(h.stream.ID)
		h.stream = nil
	}

	if h.file != nil {
		if err := h.file.Close(); err != nil {
			h.logger.ErrorContext(ctx, "Close failed", "path", h.path, "error", err)
		}
	}

	return 0
}
