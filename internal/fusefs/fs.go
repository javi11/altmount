//go:build fuse

package fusefs

import (
	"context"
	"fmt"
	"log/slog"
	"syscall" // Import syscall
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/pool"
)

// NzbFuseFs implements the fs.Inode interface for our FUSE filesystem
type NzbFuseFs struct {
	fs.Inode
	metadataRemoteFile *nzbfilesystem.MetadataRemoteFile
	readahead          int
}

var _ fs.InodeEmbedder = (*NzbFuseFs)(nil)

// NewNzbFuseFs creates a new FUSE filesystem instance
func NewNzbFuseFs(
	cfg *config.Config,
	readaheadBytes int,
	db *database.DB,
	poolManager pool.Manager,
) (*NzbFuseFs, error) {
	// Initialize dependencies for MetadataRemoteFile
	metadataService := metadata.NewMetadataService(cfg.Metadata.RootPath)
	
	// Create health repository using the database connection
	healthRepository := database.NewHealthRepository(db.Connection())

	// Create config getter closure
	configGetter := func() *config.Config {
		return cfg
	}

	metadataRemoteFile := nzbfilesystem.NewMetadataRemoteFile(
		metadataService,
		healthRepository,
		poolManager,
		configGetter,
	)

	return &NzbFuseFs{
		metadataRemoteFile: metadataRemoteFile,
		readahead:          readaheadBytes,
	}, nil
}

// Mount mounts the FUSE filesystem and returns the server instance
func (nfs *NzbFuseFs) Mount(mountpoint string) (*fuse.Server, error) {
	root := nfs.NewInode(context.Background(), nil, fs.StableAttr{Mode: fuse.S_IFDIR})
	sec := time.Second
	server, err := fs.Mount(mountpoint, root, &fs.Options{
		AttrTimeout:  &sec,
		EntryTimeout: &sec,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to mount FUSE: %w", err)
	}

	slog.Info("FUSE filesystem mounted.", "mountpoint", mountpoint)
	return server, nil
}

// Lookup implements fs.Inode.Lookup
func (nfs *NzbFuseFs) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	ok, info, err := nfs.metadataRemoteFile.Stat(ctx, name)
	if err != nil {
		slog.Error("Failed to stat file during lookup", "name", name, "error", err)
		return nil, syscall.EIO
	}
	if !ok {
		return nil, syscall.ENOENT
	}

	stableAttr := fs.StableAttr{
		Mode: uint32(info.Mode()),
		Ino:  nfs.StableAttr().Ino, // Use the root inode for now, will generate unique later
	}
	out.Attr.Mode = uint32(info.Mode())
	out.Attr.Size = uint64(info.Size())
	out.Attr.Mtime = uint64(info.ModTime().Unix())
	out.Attr.Ctime = uint64(info.ModTime().Unix())
	out.Attr.Atime = uint64(info.ModTime().Unix())

	var inode *fs.Inode
	if info.IsDir() {
		// For directories, we can return the same NzbFuseFs inode, or a dedicated directory inode
		// For simplicity, let's create a new Inode with NzbDir operations
		dir := &NzbDir{
			metadataRemoteFile: nfs.metadataRemoteFile,
			path:               name,
		}
		inode = nfs.NewInode(ctx, dir, stableAttr)
	} else {
		// For files, create a new Inode with NzbFile operations
		file := &NzbFile{
			metadataRemoteFile: nfs.metadataRemoteFile,
			path:               name,
		}
		inode = nfs.NewInode(ctx, file, stableAttr)
	}

	return inode, fs.OK
}

// Getattr implements fs.Inode.Getattr
func (nfs *NzbFuseFs) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.Attr) syscall.Errno {
	// The path for getattr is typically handled by the Inode itself, but here we're implementing it on the root
	// We'll need to figure out the path from the context or the filehandle.
	// For now, let's assume this Getattr is called on the root of the FUSE filesystem.
	// If it's a filehandle, it means it's already an opened file.

	// This Getattr will primarily be called for the root directory.
	// For specific files/directories, the Inode associated with them will have its own Getattr.

	// For the root directory, we return attributes for a directory
	out.Mode = fuse.S_IFDIR | 0755
	out.Size = 4096 // Typical directory size
	out.Nlink = 1
	out.Atime = uint64(time.Now().Unix())
	out.Mtime = uint64(time.Now().Unix())
	out.Ctime = uint64(time.Now().Unix())

	return fs.OK
}

// Readdir implements fs.Inode.Readdir
func (nfs *NzbFuseFs) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	// Delegate to an NzbDir instance representing the root
	rootNzbDir := &NzbDir{
		metadataRemoteFile: nfs.metadataRemoteFile,
		path:               "/", // Root path
	}
	return rootNzbDir.Readdir(ctx)
}

// Open implements fs.Inode.Open
func (nfs *NzbFuseFs) Open(ctx context.Context, flags uint32) (fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	// For the root directory, only read-only access is allowed.
	if flags&syscall.O_ACCMODE != syscall.O_RDONLY {
		return nil, 0, syscall.EACCES
	}
	// No special file handle needed for the root directory itself
	return nil, fuse.FOPEN_KEEP_CACHE, fs.OK
}
