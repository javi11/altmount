package fuse

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	fusecache "github.com/javi11/altmount/internal/fuse/cache"
	"github.com/spf13/afero"
)

// ensure AltMountRoot implements fs.Node* interfaces
var _ fs.NodeOnAdder = (*AltMountRoot)(nil)
var _ fs.NodeReaddirer = (*AltMountRoot)(nil)
var _ fs.NodeLookuper = (*AltMountRoot)(nil)
var _ fs.NodeGetattrer = (*AltMountRoot)(nil)
var _ fs.NodeRenamer = (*AltMountRoot)(nil)
var _ fs.NodeSetattrer = (*AltMountRoot)(nil)

// AltMountRoot represents a directory in the FUSE filesystem
type AltMountRoot struct {
	fs.Inode
	fs        afero.Fs
	path      string
	logger    *slog.Logger
	isRootDir bool
	uid       uint32
	gid       uint32
	cache     fusecache.Cache // Metadata cache (can be nil if disabled)
}

// NewAltMountRoot creates a new root node for the FUSE filesystem
func NewAltMountRoot(fileSystem afero.Fs, path string, logger *slog.Logger, uid, gid uint32, cache fusecache.Cache) *AltMountRoot {
	return &AltMountRoot{
		fs:        fileSystem,
		path:      path,
		logger:    logger,
		isRootDir: path == "" || path == "/",
		uid:       uid,
		gid:       gid,
		cache:     cache,
	}
}

// OnAdd is called when the node is added to the inode tree
func (r *AltMountRoot) OnAdd(ctx context.Context) {
	// No-op for now
}

// Getattr implements fs.NodeGetattrer
func (r *AltMountRoot) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	if r.isRootDir {
		out.Mode = 0755 | syscall.S_IFDIR
		out.Uid = r.uid
		out.Gid = r.gid
		out.Ino = 1 // Root usually has Ino 1
		return 0
	}

	// Check cache first
	if r.cache != nil {
		if info, ok := r.cache.GetStat(r.path); ok {
			fillAttr(info, &out.Attr, r.uid, r.gid)
			out.Ino = r.Inode.StableAttr().Ino
			return 0
		}
	}

	info, err := r.fs.Stat(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Cache negative result
			if r.cache != nil {
				r.cache.SetNegative(r.path)
			}
			return syscall.ENOENT
		}
		r.logger.ErrorContext(ctx, "Getattr failed", "path", r.path, "error", err)
		return syscall.EIO
	}

	// Cache the result
	if r.cache != nil {
		r.cache.SetStat(r.path, info)
	}

	fillAttr(info, &out.Attr, r.uid, r.gid)
	out.Ino = r.Inode.StableAttr().Ino
	return 0
}

// Setattr implements fs.NodeSetattrer (no-op success for renames/moves)
func (r *AltMountRoot) Setattr(ctx context.Context, f fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	return r.Getattr(ctx, f, out)
}

// Lookup implements fs.NodeLookuper
func (r *AltMountRoot) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	fullPath := filepath.Join(r.path, name)

	// Check negative cache first (known non-existent paths)
	if r.cache != nil && r.cache.IsNegative(fullPath) {
		return nil, syscall.ENOENT
	}

	// Check stat cache
	var info os.FileInfo
	var cached bool
	if r.cache != nil {
		info, cached = r.cache.GetStat(fullPath)
	}

	if !cached {
		var err error
		info, err = r.fs.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				// Cache negative result
				if r.cache != nil {
					r.cache.SetNegative(fullPath)
				}
				return nil, syscall.ENOENT
			}
			r.logger.ErrorContext(ctx, "Lookup failed", "path", fullPath, "error", err)
			return nil, syscall.EIO
		}

		// Cache the result
		if r.cache != nil {
			r.cache.SetStat(fullPath, info)
		}
	}

	fillAttr(info, &out.Attr, r.uid, r.gid)

	if info.IsDir() {
		node := &AltMountRoot{
			fs:     r.fs,
			path:   fullPath,
			logger: r.logger,
			uid:    r.uid,
			gid:    r.gid,
			cache:  r.cache,
		}
		return r.NewInode(ctx, node, fs.StableAttr{Mode: fuse.S_IFDIR}), 0
	}

	node := &AltMountFile{
		fs:     r.fs,
		path:   fullPath,
		logger: r.logger,
		size:   info.Size(),
		uid:    r.uid,
		gid:    r.gid,
		cache:  r.cache,
	}
	return r.NewInode(ctx, node, fs.StableAttr{Mode: fuse.S_IFREG}), 0
}

// Rename implements fs.NodeRenamer
func (r *AltMountRoot) Rename(ctx context.Context, oldName string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	oldPath := filepath.Join(r.path, oldName)

	targetDir, ok := newParent.(*AltMountRoot)
	if !ok {
		return syscall.EINVAL
	}
	newPath := filepath.Join(targetDir.path, newName)

	if err := r.fs.Rename(oldPath, newPath); err != nil {
		if os.IsNotExist(err) {
			return syscall.ENOENT
		}
		r.logger.ErrorContext(ctx, "Rename failed", "old", oldPath, "new", newPath, "error", err)
		return syscall.EIO
	}

	// Invalidate cache for affected paths
	if r.cache != nil {
		r.cache.Invalidate(oldPath)
		r.cache.Invalidate(newPath)
		r.cache.Invalidate(r.path)         // Source parent directory
		r.cache.Invalidate(targetDir.path) // Target parent directory
	}

	return 0
}

// Readdir implements fs.NodeReaddirer
func (r *AltMountRoot) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	// Check directory cache first
	if r.cache != nil {
		if cachedEntries, ok := r.cache.GetDirEntries(r.path); ok {
			entries := make([]fuse.DirEntry, 0, len(cachedEntries))
			for _, ce := range cachedEntries {
				mode := uint32(ce.Mode)
				if ce.IsDir {
					mode |= syscall.S_IFDIR
				} else {
					mode |= syscall.S_IFREG
				}
				entries = append(entries, fuse.DirEntry{
					Name: ce.Name,
					Mode: mode,
				})
			}
			return fs.NewListDirStream(entries), 0
		}
	}

	// Open the directory
	f, err := r.fs.Open(r.path)
	if err != nil {
		r.logger.ErrorContext(ctx, "Readdir open failed", "path", r.path, "error", err)
		return nil, syscall.EIO
	}
	defer f.Close()

	// Read all directory entries
	infos, err := f.Readdir(-1)
	if err != nil {
		r.logger.ErrorContext(ctx, "Readdir failed", "path", r.path, "error", err)
		return nil, syscall.EIO
	}

	entries := make([]fuse.DirEntry, 0, len(infos))
	cacheEntries := make([]fusecache.DirEntry, 0, len(infos))

	for _, info := range infos {
		mode := uint32(info.Mode())
		// Ensure the mode bits are set correctly for FUSE
		if info.IsDir() {
			mode |= syscall.S_IFDIR
		} else {
			mode |= syscall.S_IFREG
		}

		entries = append(entries, fuse.DirEntry{
			Name: info.Name(),
			Mode: mode,
		})

		// Build cache entry
		cacheEntries = append(cacheEntries, fusecache.DirEntry{
			Name:  info.Name(),
			IsDir: info.IsDir(),
			Mode:  info.Mode(),
		})

		// Also cache the stat for each child entry
		if r.cache != nil {
			childPath := filepath.Join(r.path, info.Name())
			r.cache.SetStat(childPath, info)
		}
	}

	// Cache directory entries
	if r.cache != nil {
		r.cache.SetDirEntries(r.path, cacheEntries)
	}

	return fs.NewListDirStream(entries), 0
}

// ensure AltMountFile implements fs.Node* interfaces
var _ fs.NodeOpener = (*AltMountFile)(nil)
var _ fs.NodeGetattrer = (*AltMountFile)(nil)
var _ fs.NodeReader = (*AltMountFile)(nil)
var _ fs.NodeSetattrer = (*AltMountFile)(nil)

// ensure FileHandle implements fs.FileReleaser
var _ fs.FileReleaser = (*FileHandle)(nil)

// Removed fs.FileReader interface check for FileHandle intentionally.
// We use Seek+Read exclusively (not ReadAt) because MetadataVirtualFile's
// UsenetReader is forward-only. ReadAt creates a new reader per call which
// causes corruption when streaming video files.

// AltMountFile represents a file in the FUSE filesystem
type AltMountFile struct {
	fs.Inode
	fs     afero.Fs
	path   string
	logger *slog.Logger
	size   int64
	uid    uint32
	gid    uint32
	cache  fusecache.Cache // Metadata cache (can be nil if disabled)
}

// Getattr implements fs.NodeGetattrer
func (f *AltMountFile) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	// Check cache first
	if f.cache != nil {
		if info, ok := f.cache.GetStat(f.path); ok {
			fillAttr(info, &out.Attr, f.uid, f.gid)
			out.Ino = f.Inode.StableAttr().Ino
			return 0
		}
	}

	info, err := f.fs.Stat(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Cache negative result
			if f.cache != nil {
				f.cache.SetNegative(f.path)
			}
			return syscall.ENOENT
		}
		f.logger.ErrorContext(ctx, "File Getattr failed", "path", f.path, "error", err)
		return syscall.EIO
	}

	// Cache the result
	if f.cache != nil {
		f.cache.SetStat(f.path, info)
	}

	fillAttr(info, &out.Attr, f.uid, f.gid)
	out.Ino = f.Inode.StableAttr().Ino
	return 0
}

// Setattr implements fs.NodeSetattrer (no-op success)
func (f *AltMountFile) Setattr(ctx context.Context, fh fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	return f.Getattr(ctx, fh, out)
}

// Open implements fs.NodeOpener
func (f *AltMountFile) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	// We only support read-only access for now
	if flags&syscall.O_ACCMODE != syscall.O_RDONLY {
		return nil, 0, syscall.EACCES
	}

	aferoFile, err := f.fs.Open(f.path)
	if err != nil {
		f.logger.ErrorContext(ctx, "File Open failed", "path", f.path, "error", err)
		return nil, 0, syscall.EIO
	}

	// Wrap the file in a handle that handles locking for Seek/Read atomicity
	handle := &FileHandle{
		file:   aferoFile,
		logger: f.logger,
		path:   f.path,
	}

	// Optimistic warm-up for faster playback start
	if warmable, ok := aferoFile.(interface{ WarmUp() }); ok {
		warmable.WarmUp()
	}

	return handle, fuse.FOPEN_KEEP_CACHE, 0
}

// Read implements fs.NodeReader
func (f *AltMountFile) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	handle := fh.(*FileHandle)
	return handle.Read(ctx, dest, off)
}

// FileHandle handles file operations.
// Uses Seek+Read exclusively (not ReadAt) because the underlying
// MetadataVirtualFile's UsenetReader is forward-only. Each ReadAt
// call would create a new reader, causing data corruption for
// streaming media files.
type FileHandle struct {
	file     afero.File
	mu       sync.Mutex
	logger   *slog.Logger
	path     string
	position int64 // Track current file position to skip unnecessary seeks
}

func (h *FileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
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

	// Read into the buffer
	n, err := h.file.Read(dest)
	if err != nil && err != io.EOF {
		h.logger.ErrorContext(ctx, "Read failed", "path", h.path, "offset", off, "size", len(dest), "error", err)
		return nil, syscall.EIO
	}

	h.position += int64(n) // Update position after read
	return fuse.ReadResultData(dest[:n]), 0
}

// Release closes the file when the handle is released
func (h *FileHandle) Release(ctx context.Context) syscall.Errno {
	err := h.file.Close()
	if err != nil {
		h.logger.ErrorContext(ctx, "Close failed", "path", h.path, "error", err)
		// We usually don't return error on Release as it's too late for the app
	}
	return 0
}

// helper to fill FUSE attributes from os.FileInfo
func fillAttr(info os.FileInfo, out *fuse.Attr, uid, gid uint32) {
	out.Size = uint64(info.Size())
	out.Mtime = uint64(info.ModTime().Unix())
	out.Ctime = uint64(info.ModTime().Unix())
	out.Atime = uint64(info.ModTime().Unix())
	out.Uid = uid
	out.Gid = gid

	// Set block information (standard block size is 512 bytes)
	out.Blksize = 4096
	out.Blocks = (out.Size + 511) / 512

	// Set generic permissions and type
	if info.IsDir() {
		out.Mode = 0755 | syscall.S_IFDIR
		out.Nlink = 2 // Directories have at least 2 links (. and parent)
	} else {
		out.Mode = 0644 | syscall.S_IFREG
		out.Nlink = 1
	}
}
