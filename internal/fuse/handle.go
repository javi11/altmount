package fuse

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/altmount/internal/fuse/vfs"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/spf13/afero"
)

// ensure Handle implements fs.FileReleaser
var _ fs.FileReleaser = (*Handle)(nil)

// Handle wraps either a VFS CachedFile (ReadAt) or an afero.File (Seek+Read).
// Uses atomic closed state to prevent double-close.
type Handle struct {
	cachedFile    *vfs.CachedFile              // Used when VFS enabled (ReadAt, no mutex needed)
	file          afero.File                   // Fallback when VFS disabled (Seek+Read)
	closed        atomic.Bool
	logger        *slog.Logger
	path          string
	vfsm          *vfs.Manager                 // For notifying close (nil in fallback mode)
	stream        *nzbfilesystem.ActiveStream  // FUSE-level stream for progress tracking
	streamTracker StreamTracker                // For UpdateProgress/Remove (nil if no tracker)

	// Only used for fallback Seek+Read mode
	mu       sync.Mutex
	position int64
}

// Read handles a read request, using either VFS ReadAt or fallback Seek+Read.
func (h *Handle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if h.closed.Load() {
		return nil, syscall.EIO
	}

	// VFS mode: use ReadAt (position-independent, no mutex needed)
	if h.cachedFile != nil {
		n, err := h.cachedFile.ReadAt(dest, off)
		if n > 0 && h.stream != nil {
			h.streamTracker.UpdateProgress(h.stream.ID, int64(n))
			atomic.StoreInt64(&h.stream.CurrentOffset, off+int64(n))
		}
		if err != nil {
			if err == io.EOF {
				if n > 0 {
					return fuse.ReadResultData(dest[:n]), 0
				}
				return fuse.ReadResultData(nil), 0
			}
			h.logger.ErrorContext(ctx, "VFS Read failed", "path", h.path, "offset", off, "error", err)
			return nil, syscall.EIO
		}
		return fuse.ReadResultData(dest[:n]), 0
	}

	// Fallback mode: Seek+Read with mutex serialization
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

	if h.cachedFile != nil {
		if err := h.cachedFile.Close(); err != nil {
			h.logger.ErrorContext(ctx, "VFS Close failed", "path", h.path, "error", err)
		}
		if h.vfsm != nil {
			h.vfsm.Close(h.path)
		}
		return 0
	}

	if h.file != nil {
		if err := h.file.Close(); err != nil {
			h.logger.ErrorContext(ctx, "Close failed", "path", h.path, "error", err)
		}
	}

	return 0
}
