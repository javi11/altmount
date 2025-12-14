//go:build fuse

package fusefs

import (
	"context"
	"log/slog"
	"path/filepath"
	"syscall"

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
	uid                uint32
	gid                uint32
}

var _ = (fs.InodeEmbedder)((*NzbDir)(nil))

// Lookup implements fs.Inode.Lookup
func (d *NzbDir) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	fullPath := filepath.Join(d.path, name)
	ok, info, err := d.metadataRemoteFile.Stat(ctx, fullPath)
	if err != nil {
		slog.Error("Failed to stat file during lookup", "path", fullPath, "error", err)
		return nil, syscall.EIO
	}
	if !ok {
		return nil, syscall.ENOENT
	}

	stableAttr := fs.StableAttr{
		Mode: ToFuseMode(info.Mode()),
		// Ino will be generated automatically if not set, or we can try to be consistent
	}
	out.Attr.Mode = ToFuseMode(info.Mode())
	out.Attr.Size = uint64(info.Size())
	out.Attr.Mtime = uint64(info.ModTime().Unix())
	out.Attr.Ctime = uint64(info.ModTime().Unix())
	out.Attr.Atime = uint64(info.ModTime().Unix())
	out.Attr.Uid = d.uid
	out.Attr.Gid = d.gid

	var inode *fs.Inode
	if info.IsDir() {
		// For directories
		dir := &NzbDir{
			metadataRemoteFile: d.metadataRemoteFile,
			path:               fullPath,
			uid:                d.uid,
			gid:                d.gid,
		}
		inode = d.NewInode(ctx, dir, stableAttr)
	} else {
		// For files
		file := &NzbFile{
			metadataRemoteFile: d.metadataRemoteFile,
			path:               fullPath,
			uid:                d.uid,
			gid:                d.gid,
		}
		inode = d.NewInode(ctx, file, stableAttr)
	}

	return inode, fs.OK
}

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
		var entryMode uint32
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

	return fs.NewListDirStream(entries), fs.OK
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

	out.Mode = ToFuseMode(info.Mode())
	out.Size = uint64(info.Size())
	out.Mtime = uint64(info.ModTime().Unix())
	out.Ctime = uint64(info.ModTime().Unix())
	out.Atime = uint64(info.ModTime().Unix())
	out.Uid = d.uid
	out.Gid = d.gid

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