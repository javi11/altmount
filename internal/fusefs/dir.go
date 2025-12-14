//go:build fuse

package fusefs

import (
	"context"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/altmount/internal/nzbfilesystem"
)

// NzbDir represents a directory in our FUSE filesystem.
// It embeds fs.Inode for standard FUSE inode behavior.
type NzbDir struct {
	fs.Inode
	metadataRemoteFile *nzbfilesystem.MetadataRemoteFile
	path               string // The full path of this directory in the virtual filesystem
}

var _ = (fs.InodeEmbedder)((*NzbDir)(nil))

// Readdir implements fs.Inode.Readdir.
// It reads the contents of the directory using the MetadataRemoteFile.
func (d *NzbDir) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	// Open the directory as an afero.File to use its Readdir method
	ok, aferoFile, err := d.metadataRemoteFile.OpenFile(ctx, d.path)
	if err != nil {
		return nil, syscall.EIO
	}
	if !ok {
		return nil, syscall.ENOENT
	}
	defer aferoFile.Close()

	// Use aferoFile.Readdir to get the directory contents
	fileInfos, err := aferoFile.Readdir(-1) // Read all entries
	if err != nil {
		return nil, syscall.EIO
	}

	entries := make([]fuse.DirEntry, len(fileInfos))
	for i, info := range fileInfos {
		entryMode := info.Mode()
		if info.IsDir() {
			entryMode = fuse.S_IFDIR
		} else {
			entryMode = fuse.S_IFREG
		}

		entries[i] = fuse.DirEntry{
			Mode: entryMode,
			Name: info.Name(),
		}
	}

	return fs.NewDirStream(entries), fs.OK
}

// Getattr implements fs.Inode.Getattr.
// It retrieves attributes for the directory using MetadataRemoteFile.
func (d *NzbDir) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.Attr) syscall.Errno {
	ok, info, err := d.metadataRemoteFile.Stat(ctx, d.path)
	if err != nil {
		return syscall.EIO
	}
	if !ok {
		return syscall.ENOENT
	}

	out.Mode = info.Mode()
	out.Size = uint64(info.Size())
	out.Mtime = uint64(info.ModTime().Unix())
	out.Ctime = uint64(info.ModTime().Unix())
	out.Atime = uint64(info.ModTime().Unix())

	return fs.OK
}

// Open implements fs.Inode.Open.
// For directories, it essentially means a successful lookup and Readdir is handled by Readdir.
func (d *NzbDir) Open(ctx context.Context, flags uint32) (fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	// Directories are always opened read-only
	if flags&syscall.O_ACCMODE != syscall.O_RDONLY {
		return nil, 0, syscall.EACCES
	}
	// No special file handle needed for directory opens for now
	return nil, fuse.FOPEN_KEEP_CACHE, fs.OK
}