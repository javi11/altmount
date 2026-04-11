//go:build linux

package hanwen

import (
	"context"
	"errors"
	"io"
	"log/slog"
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

// readAtContexter matches nzbfilesystem.MetadataVirtualFile.ReadAtContext.
type readAtContexter interface {
	ReadAtContext(ctx context.Context, p []byte, off int64) (n int, err error)
}

// Handle wraps an afero.File and serves FUSE reads via ReadAtContext (preferred)
// or io.ReaderAt. No per-handle lock needed: ReadAtContext serializes internally.
type Handle struct {
	file   afero.File
	closed atomic.Bool
	logger *slog.Logger
	path   string

	stream        *nzbfilesystem.ActiveStream
	streamTracker backend.StreamTracker
}

// NewHandle creates a Handle for the given file.
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

// Read handles a FUSE read request using offset-native ReadAtContext.
// No per-handle lock needed: ReadAtContext serializes internally via mvf.mu.
func (h *Handle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if h.closed.Load() {
		return nil, syscall.EIO
	}

	var n int
	var err error

	if rac, ok := h.file.(readAtContexter); ok {
		n, err = rac.ReadAtContext(ctx, dest, off)
	} else if ra, ok := h.file.(io.ReaderAt); ok {
		n, err = ra.ReadAt(dest, off)
	} else {
		h.logger.ErrorContext(ctx, "file does not implement ReadAtContext or io.ReaderAt", "path", h.path)
		return nil, syscall.EIO
	}

	if n > 0 {
		newPos := off + int64(n)
		if h.stream != nil {
			h.streamTracker.UpdateProgress(h.stream.ID, int64(n))
			atomic.StoreInt64(&h.stream.CurrentOffset, newPos)
		}
	}

	if err != nil && err != io.EOF {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			h.logger.DebugContext(ctx, "ReadAt canceled", "path", h.path, "offset", off)
			return nil, syscall.EINTR
		}
		h.logger.ErrorContext(ctx, "ReadAt failed", "path", h.path, "offset", off, "size", len(dest), "error", err)
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

// Release closes the file. It is idempotent.
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
