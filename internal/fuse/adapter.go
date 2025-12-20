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
}

// NewAltMountRoot creates a new root node for the FUSE filesystem
func NewAltMountRoot(fileSystem afero.Fs, path string, logger *slog.Logger, uid, gid uint32) *AltMountRoot {
	return &AltMountRoot{
		fs:        fileSystem,
		path:      path,
		logger:    logger,
		isRootDir: path == "" || path == "/",
		uid:       uid,
		gid:       gid,
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

	info, err := r.fs.Stat(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return syscall.ENOENT
		}
		r.logger.ErrorContext(ctx, "Getattr failed", "path", r.path, "error", err)
		return syscall.EIO
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

	info, err := r.fs.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, syscall.ENOENT
		}
		r.logger.ErrorContext(ctx, "Lookup failed", "path", fullPath, "error", err)
		return nil, syscall.EIO
	}

	fillAttr(info, &out.Attr, r.uid, r.gid)

	if info.IsDir() {
		node := &AltMountRoot{
			fs:     r.fs,
			path:   fullPath,
			logger: r.logger,
			uid:    r.uid,
			gid:    r.gid,
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

	return 0
}

// Readdir implements fs.NodeReaddirer
func (r *AltMountRoot) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
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

// AltMountFile represents a file in the FUSE filesystem
type AltMountFile struct {
	fs.Inode
	fs     afero.Fs
	path   string
	logger *slog.Logger
	size   int64
	uid    uint32
	gid    uint32
}

// Getattr implements fs.NodeGetattrer
func (f *AltMountFile) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	info, err := f.fs.Stat(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return syscall.ENOENT
		}
		f.logger.ErrorContext(ctx, "File Getattr failed", "path", f.path, "error", err)
		return syscall.EIO
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

// FileHandle handles file operations
type FileHandle struct {
	file   afero.File
	mu     sync.Mutex
	logger *slog.Logger
	path   string
}

// Read implements the actual reading logic with Seek+Read atomicity
func (h *FileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Seek to the requested offset
	_, err := h.file.Seek(off, io.SeekStart)
	if err != nil {
		h.logger.ErrorContext(ctx, "Seek failed", "path", h.path, "offset", off, "error", err)
		return nil, syscall.EIO
	}

	// Read into the buffer
	n, err := h.file.Read(dest)
	if err != nil && err != io.EOF {
		h.logger.ErrorContext(ctx, "Read failed", "path", h.path, "offset", off, "size", len(dest), "error", err)
		return nil, syscall.EIO
	}

	// Return the data read
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