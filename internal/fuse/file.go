package fuse

import (
	"context"
	"log/slog"
	"os"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/altmount/internal/fuse/vfs"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/utils"
	"github.com/spf13/afero"
)

// StreamTracker is the subset of stream tracking needed by the FUSE layer.
// *api.StreamTracker satisfies this interface.
type StreamTracker interface {
	AddStream(filePath, source, userName, clientIP, userAgent string, totalSize int64) *nzbfilesystem.ActiveStream
	UpdateProgress(id string, bytesRead int64)
	Remove(id string)
}

// suppressStreamOpener wraps a FileOpener to inject SuppressStreamTrackingKey
// so that backend opens (VFS cache-miss fetches) don't create their own streams.
type suppressStreamOpener struct {
	inner vfs.FileOpener
}

func (o *suppressStreamOpener) Open(ctx context.Context, name string) (afero.File, error) {
	ctx = context.WithValue(ctx, utils.SuppressStreamTrackingKey, true)
	return o.inner.Open(ctx, name)
}

// ensure File implements fs.Node* interfaces
var _ fs.NodeOpener = (*File)(nil)
var _ fs.NodeGetattrer = (*File)(nil)
var _ fs.NodeReader = (*File)(nil)
var _ fs.NodeSetattrer = (*File)(nil)

// File represents a file in the FUSE filesystem.
// Talks directly to NzbFilesystem with FUSE context propagation.
type File struct {
	fs.Inode
	nzbfs         *nzbfilesystem.NzbFilesystem
	vfsm          *vfs.Manager // VFS disk cache manager (nil if disabled)
	streamTracker StreamTracker
	path          string
	logger        *slog.Logger
	size          int64
	uid           uint32
	gid           uint32
}

// Getattr implements fs.NodeGetattrer.
func (f *File) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	info, err := f.nzbfs.Stat(ctx, f.path)
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

// Setattr implements fs.NodeSetattrer (no-op success).
func (f *File) Setattr(ctx context.Context, fh fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	return f.Getattr(ctx, fh, out)
}

// Open implements fs.NodeOpener.
func (f *File) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	// Only support read-only access
	if flags&syscall.O_ACCMODE != syscall.O_RDONLY {
		return nil, 0, syscall.EACCES
	}

	// Create a FUSE-level stream (one per file open) that lives for the
	// duration of the handle. Backend opens will be told to suppress their
	// own stream creation via SuppressStreamTrackingKey.
	var stream *nzbfilesystem.ActiveStream
	if f.streamTracker != nil {
		stream = f.streamTracker.AddStream(f.path, "FUSE", "FUSE", "", "", f.size)
	}

	// Suppress backend stream creation for all downstream opens
	ctx = context.WithValue(ctx, utils.SuppressStreamTrackingKey, true)

	// VFS mode: use disk cache with ReadAt support
	if f.vfsm != nil {
		opener := &suppressStreamOpener{inner: &nzbfsFileOpener{nzbfs: f.nzbfs}}
		cachedFile, err := f.vfsm.Open(ctx, f.path, f.size, opener)
		if err != nil {
			// Clean up stream on failure
			if stream != nil {
				f.streamTracker.Remove(stream.ID)
			}
			f.logger.ErrorContext(ctx, "VFS Open failed", "path", f.path, "error", err)
			return nil, 0, syscall.EIO
		}

		handle := &Handle{
			cachedFile:    cachedFile,
			logger:        f.logger,
			path:          f.path,
			vfsm:          f.vfsm,
			stream:        stream,
			streamTracker: f.streamTracker,
		}
		return handle, fuse.FOPEN_KEEP_CACHE, 0
	}

	// Fallback mode: direct file access with Seek+Read
	aferoFile, err := f.nzbfs.Open(ctx, f.path)
	if err != nil {
		// Clean up stream on failure
		if stream != nil {
			f.streamTracker.Remove(stream.ID)
		}
		f.logger.ErrorContext(ctx, "File Open failed", "path", f.path, "error", err)
		return nil, 0, syscall.EIO
	}

	// Optimistic warm-up for faster playback start
	if warmable, ok := aferoFile.(interface{ WarmUp() }); ok {
		warmable.WarmUp()
	}

	handle := &Handle{
		file:          aferoFile,
		logger:        f.logger,
		path:          f.path,
		stream:        stream,
		streamTracker: f.streamTracker,
	}
	return handle, fuse.FOPEN_KEEP_CACHE, 0
}

// Read implements fs.NodeReader.
func (f *File) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	handle := fh.(*Handle)
	return handle.Read(ctx, dest, off)
}

// nzbfsFileOpener adapts NzbFilesystem to vfs.FileOpener interface.
type nzbfsFileOpener struct {
	nzbfs *nzbfilesystem.NzbFilesystem
}

func (o *nzbfsFileOpener) Open(ctx context.Context, name string) (afero.File, error) {
	return o.nzbfs.Open(ctx, name)
}
