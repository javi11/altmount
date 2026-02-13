package fuse

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/altmount/internal/fuse/vfs"
	"github.com/javi11/altmount/internal/nzbfilesystem"
)

// ensure Dir implements fs.Node* interfaces
var _ fs.NodeOnAdder = (*Dir)(nil)
var _ fs.NodeReaddirer = (*Dir)(nil)
var _ fs.NodeLookuper = (*Dir)(nil)
var _ fs.NodeGetattrer = (*Dir)(nil)
var _ fs.NodeRenamer = (*Dir)(nil)
var _ fs.NodeSetattrer = (*Dir)(nil)

// Dir represents a directory in the FUSE filesystem.
// Talks directly to NzbFilesystem with FUSE context propagation.
type Dir struct {
	fs.Inode
	nzbfs         *nzbfilesystem.NzbFilesystem
	vfsm          *vfs.Manager // VFS disk cache manager (nil if disabled)
	streamTracker StreamTracker
	path          string
	logger        *slog.Logger
	isRootDir     bool
	uid           uint32
	gid           uint32
}

// NewDir creates a new root directory node for the FUSE filesystem.
func NewDir(nzbfs *nzbfilesystem.NzbFilesystem, path string, logger *slog.Logger, uid, gid uint32, vfsm *vfs.Manager, st StreamTracker) *Dir {
	return &Dir{
		nzbfs:         nzbfs,
		vfsm:          vfsm,
		streamTracker: st,
		path:          path,
		logger:        logger,
		isRootDir:     path == "" || path == "/",
		uid:           uid,
		gid:           gid,
	}
}

// OnAdd is called when the node is added to the inode tree.
func (d *Dir) OnAdd(ctx context.Context) {
	// No-op
}

// Getattr implements fs.NodeGetattrer.
func (d *Dir) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	if d.isRootDir {
		out.Mode = 0755 | syscall.S_IFDIR
		out.Uid = d.uid
		out.Gid = d.gid
		out.Ino = 1
		return 0
	}

	info, err := d.nzbfs.Stat(ctx, d.path)
	if err != nil {
		if os.IsNotExist(err) {
			return syscall.ENOENT
		}
		d.logger.ErrorContext(ctx, "Getattr failed", "path", d.path, "error", err)
		return syscall.EIO
	}

	fillAttr(info, &out.Attr, d.uid, d.gid)
	out.Ino = d.Inode.StableAttr().Ino
	return 0
}

// Setattr implements fs.NodeSetattrer (delegates to Getattr).
func (d *Dir) Setattr(ctx context.Context, f fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	return d.Getattr(ctx, f, out)
}

// Lookup implements fs.NodeLookuper.
func (d *Dir) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	fullPath := filepath.Join(d.path, name)

	info, err := d.nzbfs.Stat(ctx, fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, syscall.ENOENT
		}
		d.logger.ErrorContext(ctx, "Lookup failed", "path", fullPath, "error", err)
		return nil, syscall.EIO
	}

	fillAttr(info, &out.Attr, d.uid, d.gid)

	if info.IsDir() {
		node := &Dir{
			nzbfs:         d.nzbfs,
			vfsm:          d.vfsm,
			streamTracker: d.streamTracker,
			path:          fullPath,
			logger:        d.logger,
			uid:           d.uid,
			gid:           d.gid,
		}
		return d.NewInode(ctx, node, fs.StableAttr{Mode: fuse.S_IFDIR}), 0
	}

	node := &File{
		nzbfs:         d.nzbfs,
		vfsm:          d.vfsm,
		streamTracker: d.streamTracker,
		path:          fullPath,
		logger:        d.logger,
		size:          info.Size(),
		uid:           d.uid,
		gid:           d.gid,
	}
	return d.NewInode(ctx, node, fs.StableAttr{Mode: fuse.S_IFREG}), 0
}

// Rename implements fs.NodeRenamer.
func (d *Dir) Rename(ctx context.Context, oldName string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	oldPath := filepath.Join(d.path, oldName)

	targetDir, ok := newParent.(*Dir)
	if !ok {
		return syscall.EINVAL
	}
	newPath := filepath.Join(targetDir.path, newName)

	if err := d.nzbfs.Rename(ctx, oldPath, newPath); err != nil {
		if os.IsNotExist(err) {
			return syscall.ENOENT
		}
		d.logger.ErrorContext(ctx, "Rename failed", "old", oldPath, "new", newPath, "error", err)
		return syscall.EIO
	}

	return 0
}

// Readdir implements fs.NodeReaddirer.
func (d *Dir) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	// Open directory via NzbFilesystem with FUSE context
	f, err := d.nzbfs.Open(ctx, d.path)
	if err != nil {
		d.logger.ErrorContext(ctx, "Readdir open failed", "path", d.path, "error", err)
		return nil, syscall.EIO
	}
	defer f.Close()

	infos, err := f.Readdir(-1)
	if err != nil {
		d.logger.ErrorContext(ctx, "Readdir failed", "path", d.path, "error", err)
		return nil, syscall.EIO
	}

	entries := make([]fuse.DirEntry, 0, len(infos))

	for _, info := range infos {
		mode := uint32(info.Mode())
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
