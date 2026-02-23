//go:build darwin || windows

package cgofuse

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cgofuse "github.com/winfsp/cgofuse/fuse"

	"github.com/javi11/altmount/internal/fuse/backend"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/utils"
	"github.com/spf13/afero"
)

// ensure FS implements cgofuse interfaces
var _ cgofuse.FileSystemInterface = (*FS)(nil)
var _ cgofuse.FileSystemOpenEx = (*FS)(nil)

// CreateEx is a no-op (read-only filesystem). Required by cgofuse.FileSystemOpenEx.
func (f *FS) CreateEx(path string, mode uint32, fi *cgofuse.FileInfo_t) int {
	return -cgofuse.EACCES
}

// openHandle tracks an open file and its associated stream.
// Uses mutex-protected Seek+Read to preserve UsenetReader prefetch state.
type openHandle struct {
	file   afero.File
	stream *nzbfilesystem.ActiveStream
	path   string
	closed atomic.Bool

	// Seek+Read serialization
	mu       sync.Mutex
	position int64
}

// FS implements cgofuse.FileSystemInterface using NzbFilesystem.
type FS struct {
	cgofuse.FileSystemBase

	cfg    backend.Config
	logger *slog.Logger

	// Handle management
	mu      sync.RWMutex
	handles map[uint64]*openHandle
	nextFH  uint64

	// Ready signal
	ready chan struct{}
}

// NewFS creates a new cgofuse filesystem.
func NewFS(cfg backend.Config, logger *slog.Logger) *FS {
	return &FS{
		cfg:     cfg,
		logger:  logger,
		handles: make(map[uint64]*openHandle),
		nextFH:  1,
		ready:   make(chan struct{}),
	}
}

// Ready returns a channel that is closed when Init has been called.
func (f *FS) Ready() <-chan struct{} {
	return f.ready
}

// allocHandle assigns a file handle number and stores the handle.
func (f *FS) allocHandle(h *openHandle) uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	fh := f.nextFH
	f.nextFH++
	f.handles[fh] = h
	return fh
}

// getHandle retrieves an open handle by file handle number.
func (f *FS) getHandle(fh uint64) *openHandle {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.handles[fh]
}

// removeHandle removes and returns a handle.
func (f *FS) removeHandle(fh uint64) *openHandle {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := f.handles[fh]
	delete(f.handles, fh)
	return h
}

// cleanPath normalizes a FUSE path (always starts with /).
func cleanPath(path string) string {
	if path == "/" {
		return ""
	}
	return strings.TrimPrefix(path, "/")
}

// --- Lifecycle ---

// Init is called when the filesystem is initialized.
func (f *FS) Init() {
	close(f.ready)
}

// Destroy is called when the filesystem is destroyed.
func (f *FS) Destroy() {
	f.mu.Lock()
	defer f.mu.Unlock()

	for fh, h := range f.handles {
		f.closeHandle(h)
		delete(f.handles, fh)
	}
}

func (f *FS) closeHandle(h *openHandle) {
	if !h.closed.CompareAndSwap(false, true) {
		return
	}
	if h.stream != nil && f.cfg.StreamTracker != nil {
		f.cfg.StreamTracker.Remove(h.stream.ID)
	}
	if h.file != nil {
		_ = h.file.Close()
	}
}

// --- Metadata ---

// Getattr retrieves file attributes.
func (f *FS) Getattr(path string, stat *cgofuse.Stat_t, fh uint64) int {
	clean := cleanPath(path)

	if clean == "" {
		f.fillDirStat(stat)
		return 0
	}

	ctx := context.Background()
	info, err := f.cfg.NzbFs.Stat(ctx, clean)
	if err != nil {
		if os.IsNotExist(err) {
			return -cgofuse.ENOENT
		}
		f.logger.Error("Getattr failed", "path", path, "error", err)
		return -cgofuse.EIO
	}

	f.fillStat(info, stat)
	return 0
}

// --- Directory operations ---

// Opendir opens a directory for reading.
func (f *FS) Opendir(path string) (int, uint64) {
	return 0, 0
}

// Releasedir releases a directory.
func (f *FS) Releasedir(path string, fh uint64) int {
	return 0
}

// Readdir reads directory entries.
func (f *FS) Readdir(path string, fill func(name string, stat *cgofuse.Stat_t, ofst int64) bool, ofst int64, fh uint64) int {
	clean := cleanPath(path)
	ctx := context.Background()

	dir, err := f.cfg.NzbFs.Open(ctx, clean)
	if err != nil {
		f.logger.Error("Readdir open failed", "path", path, "error", err)
		return -cgofuse.EIO
	}
	defer dir.Close()

	infos, err := dir.Readdir(-1)
	if err != nil {
		f.logger.Error("Readdir failed", "path", path, "error", err)
		return -cgofuse.EIO
	}

	// Add . and .. entries
	fill(".", nil, 0)
	fill("..", nil, 0)

	for _, info := range infos {
		var stat cgofuse.Stat_t
		f.fillStat(info, &stat)
		if !fill(info.Name(), &stat, 0) {
			break
		}
	}

	return 0
}

// Mkdir creates a directory.
func (f *FS) Mkdir(path string, mode uint32) int {
	clean := cleanPath(path)
	ctx := context.Background()

	if err := f.cfg.NzbFs.Mkdir(ctx, clean, os.FileMode(mode)); err != nil {
		f.logger.Error("Mkdir failed", "path", path, "error", err)
		return -cgofuse.EIO
	}

	return 0
}

// Rmdir removes an empty directory.
func (f *FS) Rmdir(path string) int {
	clean := cleanPath(path)
	ctx := context.Background()

	if err := f.cfg.NzbFs.Remove(ctx, clean); err != nil {
		if os.IsNotExist(err) {
			return -cgofuse.ENOENT
		}
		f.logger.Error("Rmdir failed", "path", path, "error", err)
		return -cgofuse.EIO
	}

	return 0
}

// --- File operations ---

// OpenEx opens a file with extended flags (cgofuse.FileSystemOpenEx).
func (f *FS) OpenEx(path string, fi *cgofuse.FileInfo_t) int {
	clean := cleanPath(path)
	ctx := context.Background()

	// Only support read-only
	if fi.Flags&os.O_RDWR != 0 || fi.Flags&os.O_WRONLY != 0 {
		return -cgofuse.EACCES
	}

	// Get file size for stream tracking
	info, err := f.cfg.NzbFs.Stat(ctx, clean)
	if err != nil {
		if os.IsNotExist(err) {
			return -cgofuse.ENOENT
		}
		f.logger.Error("OpenEx stat failed", "path", path, "error", err)
		return -cgofuse.EIO
	}

	var stream *nzbfilesystem.ActiveStream
	if f.cfg.StreamTracker != nil {
		stream = f.cfg.StreamTracker.AddStream(clean, "FUSE", "FUSE", "", "", info.Size())
	}

	ctx = context.WithValue(ctx, utils.SuppressStreamTrackingKey, true)

	file, err := f.cfg.NzbFs.Open(ctx, clean)
	if err != nil {
		if stream != nil {
			f.cfg.StreamTracker.Remove(stream.ID)
		}
		if os.IsNotExist(err) {
			return -cgofuse.ENOENT
		}
		f.logger.Error("OpenEx failed", "path", path, "error", err)
		return -cgofuse.EIO
	}

	// Optimistic warm-up
	if warmable, ok := file.(interface{ WarmUp() }); ok {
		warmable.WarmUp()
	}

	h := &openHandle{
		file:   file,
		stream: stream,
		path:   clean,
	}

	fi.Fh = f.allocHandle(h)
	fi.DirectIo = false
	fi.KeepCache = true

	return 0
}

// Open opens a file (fallback if OpenEx not used by runtime).
func (f *FS) Open(path string, flags int) (int, uint64) {
	fi := &cgofuse.FileInfo_t{Flags: flags}
	errc := f.OpenEx(path, fi)
	return errc, fi.Fh
}

// Read reads data from an open file using mutex-protected Seek+Read.
// This keeps the persistent UsenetReader alive across reads, allowing
// the downloadManager prefetch pipeline to stay effective.
func (f *FS) Read(path string, buff []byte, ofst int64, fh uint64) int {
	h := f.getHandle(fh)
	if h == nil {
		return -cgofuse.EBADF
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Skip seek if already at the correct position (sequential read optimization)
	if ofst != h.position {
		if _, err := h.file.Seek(ofst, io.SeekStart); err != nil {
			f.logger.Error("Read seek failed", "path", path, "offset", ofst, "error", err)
			return -cgofuse.EIO
		}
	}

	n, err := h.file.Read(buff)

	if n > 0 {
		h.position = ofst + int64(n)
		if h.stream != nil && f.cfg.StreamTracker != nil {
			f.cfg.StreamTracker.UpdateProgress(h.stream.ID, int64(n))
			atomic.StoreInt64(&h.stream.CurrentOffset, h.position)
		}
	}

	if err != nil && err != io.EOF {
		f.logger.Error("Read failed", "path", path, "offset", ofst, "error", err)
		return -cgofuse.EIO
	}

	return n
}

// Release closes an open file.
func (f *FS) Release(path string, fh uint64) int {
	h := f.removeHandle(fh)
	if h == nil {
		return 0
	}

	f.closeHandle(h)
	return 0
}

// Flush is called on each close of an open file.
func (f *FS) Flush(path string, fh uint64) int {
	return 0
}

// Fsync synchronizes file contents.
func (f *FS) Fsync(path string, datasync bool, fh uint64) int {
	return 0
}

// --- Other operations ---

// Unlink removes a file.
func (f *FS) Unlink(path string) int {
	clean := cleanPath(path)
	ctx := context.Background()

	if err := f.cfg.NzbFs.Remove(ctx, clean); err != nil {
		if os.IsNotExist(err) {
			return -cgofuse.ENOENT
		}
		f.logger.Error("Unlink failed", "path", path, "error", err)
		return -cgofuse.EIO
	}

	return 0
}

// Rename renames a file or directory.
func (f *FS) Rename(oldpath string, newpath string) int {
	ctx := context.Background()

	if err := f.cfg.NzbFs.Rename(ctx, cleanPath(oldpath), cleanPath(newpath)); err != nil {
		f.logger.Error("Rename failed", "old", oldpath, "new", newpath, "error", err)
		return -cgofuse.EIO
	}

	return 0
}

// Statfs returns filesystem statistics.
func (f *FS) Statfs(path string, stat *cgofuse.Statfs_t) int {
	const totalSize = 1024 * 1024 * 1024 * 1024 * 1024 // 1PB
	const blockSize = 4096

	stat.Blocks = totalSize / blockSize
	stat.Bfree = stat.Blocks
	stat.Bavail = stat.Blocks
	stat.Bsize = blockSize
	stat.Namemax = 255
	stat.Frsize = blockSize

	return 0
}

// --- Helpers ---

func (f *FS) fillDirStat(stat *cgofuse.Stat_t) {
	stat.Mode = cgofuse.S_IFDIR | 0755
	stat.Uid = f.cfg.UID
	stat.Gid = f.cfg.GID
	stat.Nlink = 2

	now := cgofuse.NewTimespec(time.Now())
	stat.Atim = now
	stat.Mtim = now
	stat.Ctim = now
}

func (f *FS) fillStat(info os.FileInfo, stat *cgofuse.Stat_t) {
	stat.Size = info.Size()
	stat.Uid = f.cfg.UID
	stat.Gid = f.cfg.GID

	// Block information
	stat.Blksize = 4096
	stat.Blocks = int64((uint64(info.Size()) + 511) / 512)

	mtime := cgofuse.NewTimespec(info.ModTime())
	stat.Atim = mtime
	stat.Mtim = mtime
	stat.Ctim = mtime

	if info.IsDir() {
		stat.Mode = cgofuse.S_IFDIR | 0755
		stat.Nlink = 2
	} else {
		stat.Mode = cgofuse.S_IFREG | 0644
		stat.Nlink = 1
	}
}

