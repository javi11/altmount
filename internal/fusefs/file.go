//go:build fuse

package fusefs

import (
	"context"
	"io"
	"log/slog"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/spf13/afero"
)

// NzbFile represents a file in our FUSE filesystem.
type NzbFile struct {
	fs.Inode
	metadataRemoteFile *nzbfilesystem.MetadataRemoteFile
	path               string // The full path of this file in the virtual filesystem
}

var _ = (fs.InodeEmbedder)((*NzbFile)(nil))

// NzbFileHandle implements fs.FileHandle for an opened file.
type NzbFileHandle struct {
	aferoFile afero.File // The underlying afero.File opened from metadataRemoteFile
}

var _ = (fs.FileHandle)((*NzbFileHandle)(nil))

// Getattr implements fs.Inode.Getattr.
// It retrieves attributes for the file using MetadataRemoteFile.
func (f *NzbFile) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.Attr) syscall.Errno {
	ok, info, err := f.metadataRemoteFile.Stat(ctx, f.path)
	if err != nil {
		return syscall.EIO
	}
	if !ok {
		return syscall.ENOENT
	}

	out.Mode = uint32(info.Mode())
	out.Size = uint64(info.Size())
	out.Mtime = uint64(info.ModTime().Unix())
	out.Ctime = uint64(info.ModTime().Unix())
	out.Atime = uint64(info.ModTime().Unix())

	return fs.OK
}

// Open implements fs.Inode.Open.
// It opens the file using MetadataRemoteFile and returns an NzbFileHandle.
func (f *NzbFile) Open(ctx context.Context, flags uint32) (fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	// Only allow read-only access
	if flags&syscall.O_ACCMODE != syscall.O_RDONLY {
		return nil, 0, syscall.EACCES
	}

	ok, aferoFile, err := f.metadataRemoteFile.OpenFile(ctx, f.path)
	if err != nil {
		slog.Error("Failed to open file", "path", f.path, "error", err)
		return nil, 0, syscall.EIO
	}
	if !ok {
		return nil, 0, syscall.ENOENT
	}

	return &NzbFileHandle{aferoFile: aferoFile}, fuse.FOPEN_KEEP_CACHE, fs.OK
}

// Read implements fs.FileHandle.Read.
// It reads data from the underlying afero.File.
func (fh *NzbFileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	// Seek to the correct offset
	_, err := fh.aferoFile.Seek(off, io.SeekStart)
	if err != nil {
		slog.Error("Failed to seek file", "offset", off, "error", err)
		return nil, syscall.EIO
	}

	// Read data into the destination buffer
	n, err := fh.aferoFile.Read(dest)
	if err != nil && err != io.EOF {
		slog.Error("Failed to read file", "error", err)
		return nil, syscall.EIO
	}

	return fuse.ReadResultData(dest[:n]), fs.OK
}

// Release implements fs.FileHandle.Release.
// It closes the underlying afero.File.
func (fh *NzbFileHandle) Release(ctx context.Context) syscall.Errno {
	err := fh.aferoFile.Close()
	if err != nil {
		slog.Error("Failed to close file", "error", err)
		return syscall.EIO
	}
	return fs.OK
}

// Other no-op methods for fs.FileHandle interface
func (fh *NzbFileHandle) Write(ctx context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	return 0, syscall.EROFS // Read-only filesystem
}

func (fh *NzbFileHandle) Flush(ctx context.Context) syscall.Errno {
	return fs.OK
}

func (fh *NzbFileHandle) Fsync(ctx context.Context, flags uint32) syscall.Errno {
	return fs.OK
}

func (fh *NzbFileHandle) Truncate(ctx context.Context, size uint64) syscall.Errno {
	return syscall.EROFS // Read-only filesystem
}

func (fh *NzbFileHandle) Getattr(ctx context.Context, out *fuse.Attr) syscall.Errno {
	return syscall.ENOSYS // Handled by Inode.Getattr
}

func (fh *NzbFileHandle) Utimens(ctx context.Context, a *time.Time, m *time.Time) syscall.Errno {
	return syscall.EROFS // Read-only filesystem
}

func (fh *NzbFileHandle) Allocate(ctx context.Context, off uint64, size uint64) syscall.Errno {
	return syscall.EROFS // Read-only filesystem
}