package fuse

import (
	"os"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fuse"
)

// fillAttr populates FUSE attributes from os.FileInfo.
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
