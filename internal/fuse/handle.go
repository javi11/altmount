package fuse

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
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/spf13/afero"
)

// ensure Handle implements fs.FileReleaser
var _ fs.FileReleaser = (*Handle)(nil)

// Handle wraps an afero.File with Seek+Read access.
// Uses atomic closed state to prevent double-close.
type Handle struct {
	file          afero.File
	closed        atomic.Bool
	logger        *slog.Logger
	path          string
	stream        *nzbfilesystem.ActiveStream // FUSE-level stream for progress tracking
	streamTracker StreamTracker               // For UpdateProgress/Remove (nil if no tracker)

	// Seek+Read serialization
	mu       sync.Mutex
	position int64
}

// Read handles a read request using Seek+Read.
func (h *Handle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if h.closed.Load() {
		return nil, syscall.EIO
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Only seek if position changed (skip for sequential reads)
	if off != h.position {
		_, err := h.file.Seek(off, io.SeekStart)
		if err != nil {
			h.logger.ErrorContext(ctx, "Seek failed", "path", h.path, "offset", off, "error", err)
			return nil, syscall.EIO
		}
		h.position = off
	}

	n, err := h.file.Read(dest)
	if n > 0 && h.stream != nil {
		h.streamTracker.UpdateProgress(h.stream.ID, int64(n))
		atomic.StoreInt64(&h.stream.CurrentOffset, off+int64(n))
	}
	if err != nil && err != io.EOF {
		// Context cancellation is expected (user stopped playback/closed file)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			h.logger.DebugContext(ctx, "Read canceled", "path", h.path, "offset", off)
			return nil, syscall.EINTR
		}

		h.logger.ErrorContext(ctx, "Read failed", "path", h.path, "offset", off, "size", len(dest), "error", err)
		return nil, syscall.EIO
	}

	h.position += int64(n)
	return fuse.ReadResultData(dest[:n]), 0
}

// Release closes the file when the handle is released.
func (h *Handle) Release(ctx context.Context) syscall.Errno {
	if !h.closed.CompareAndSwap(false, true) {
		return 0 // Already closed
	}

	// Remove the FUSE-level stream before closing the underlying file
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
