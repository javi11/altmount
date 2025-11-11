package webdav

import (
	"context"
	"log/slog"
	"os"

	"github.com/javi11/altmount/internal/nzbfilesystem"
	"golang.org/x/net/webdav"
)

type fileSystem struct {
	nzbFs *nzbfilesystem.NzbFilesystem
}

func nzbToWebdavFS(vfs *nzbfilesystem.NzbFilesystem) webdav.FileSystem {
	return &fileSystem{
		nzbFs: vfs,
	}
}

func (fs *fileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return fs.nzbFs.Mkdir(ctx, name, perm)
}

func (fs *fileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	// Context values are now passed directly through the context
	// No need to encode them into the path string
	return fs.nzbFs.OpenFile(ctx, name, flag, perm)
}

func (fs *fileSystem) RemoveAll(ctx context.Context, name string) error {
	return fs.nzbFs.RemoveAll(ctx, name)
}

func (fs *fileSystem) Rename(ctx context.Context, oldName, newName string) error {
	// Add logging to understand when MOVE operations trigger renames
	slog.InfoContext(ctx, "WebDAV filesystem Rename called",
		"oldName", oldName,
		"newName", newName)
	return fs.nzbFs.Rename(ctx, oldName, newName)
}

func (fs *fileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return fs.nzbFs.Stat(ctx, name)
}
