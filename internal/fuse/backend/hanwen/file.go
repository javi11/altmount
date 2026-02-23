package hanwen

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/altmount/internal/fuse/backend"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/utils"
)

// ensure File implements fs.Node* interfaces
var _ fs.NodeOpener = (*File)(nil)
var _ fs.NodeGetattrer = (*File)(nil)
var _ fs.NodeReader = (*File)(nil)
var _ fs.NodeSetattrer = (*File)(nil)

// File represents a file in the FUSE filesystem.
type File struct {
	fs.Inode
	nzbfs         *nzbfilesystem.NzbFilesystem
	streamTracker backend.StreamTracker
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
	if flags&syscall.O_ACCMODE != syscall.O_RDONLY {
		return nil, 0, syscall.EACCES
	}

	// Create a FUSE-level stream (one per file open) that lives for the
	// duration of the handle. Backend opens are told to suppress their
	// own stream creation via SuppressStreamTrackingKey.
	var stream *nzbfilesystem.ActiveStream
	if f.streamTracker != nil {
		stream = f.streamTracker.AddStream(f.path, "FUSE", "FUSE", "", "", f.size)
	}

	ctx = context.WithValue(ctx, utils.SuppressStreamTrackingKey, true)

	aferoFile, err := f.nzbfs.Open(ctx, f.path)
	if err != nil {
		if stream != nil {
			f.streamTracker.Remove(stream.ID)
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			f.logger.DebugContext(ctx, "File Open canceled", "path", f.path)
			return nil, 0, syscall.EINTR
		}

		f.logger.ErrorContext(ctx, "File Open failed", "path", f.path, "error", err)
		return nil, 0, syscall.EIO
	}

	// Optimistic warm-up for faster playback start
	if warmable, ok := aferoFile.(interface{ WarmUp() }); ok {
		warmable.WarmUp()
	}

	handle := NewHandle(aferoFile, f.logger, f.path, stream, f.streamTracker)
	return handle, fuse.FOPEN_KEEP_CACHE, 0
}

// Read implements fs.NodeReader.
func (f *File) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	handle := fh.(*Handle)
	return handle.Read(ctx, dest, off)
}
