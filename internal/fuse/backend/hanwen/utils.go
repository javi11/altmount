package hanwen

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"
	"os"
	"sync"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fuse"
)

var fnvPool = sync.Pool{
	New: func() any {
		return fnv.New64a()
	},
}

// hashPath returns a stable inode number from a path string using FNV-64a.
func hashPath(path string) uint64 {
	h := fnvPool.Get().(interface {
		Write([]byte) (int, error)
		Sum64() uint64
		Reset()
	})
	defer fnvPool.Put(h)
	h.Reset()
	_, _ = h.Write([]byte(path))
	return h.Sum64()
}

// translateError maps OS-level errors to FUSE syscall.Errno values.
// Does not log; callers should log unexpected errors before calling.
func translateError(err error) syscall.Errno {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, os.ErrNotExist):
		return syscall.ENOENT
	case errors.Is(err, os.ErrPermission):
		return syscall.EACCES
	case errors.Is(err, os.ErrExist):
		return syscall.EEXIST
	default:
		return syscall.EIO
	}
}

// mapError translates an error to a FUSE errno, logging unexpected errors.
func mapError(err error, logger *slog.Logger, ctx context.Context, msg string, args ...any) syscall.Errno {
	if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) && !errors.Is(err, os.ErrExist) {
		logArgs := append([]any{}, args...)
		logArgs = append(logArgs, "error", err)
		logger.ErrorContext(ctx, msg, logArgs...)
	}
	return translateError(err)
}

// fillAttr populates FUSE attributes from os.FileInfo.
func fillAttr(info os.FileInfo, out *fuse.Attr, uid, gid uint32) {
	out.Size = uint64(info.Size())
	out.Mtime = uint64(info.ModTime().Unix())
	out.Ctime = uint64(info.ModTime().Unix())
	out.Atime = uint64(info.ModTime().Unix())
	out.Uid = uid
	out.Gid = gid

	out.Blksize = 4096
	out.Blocks = (out.Size + 511) / 512

	if info.IsDir() {
		out.Mode = 0755 | syscall.S_IFDIR
		out.Nlink = 2
	} else {
		out.Mode = 0644 | syscall.S_IFREG
		out.Nlink = 1
	}
}
