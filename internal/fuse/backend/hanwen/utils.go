package hanwen

import (
	"hash/fnv"
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
