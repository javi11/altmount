package webdav

import (
	"context"
	"log/slog"
	"os"

	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/utils"
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
	// Build path with args from context values
	pa := utils.NewPathWithArgs(name)

	if r, ok := ctx.Value(utils.RangeKey).(string); ok && r != "" {
		pa.SetRange(r)
	}

	if s, ok := ctx.Value(utils.ContentLengthKey).(string); ok && s != "" {
		pa.SetFileSize(s)
	}

	if isCopy, ok := ctx.Value(utils.IsCopy).(bool); ok && isCopy {
		pa.SetIsCopy()
	}

	if origin, ok := ctx.Value(utils.Origin).(string); ok && origin != "" {
		pa.SetOrigin(origin)
	}

	if showCorrupted, ok := ctx.Value(utils.ShowCorrupted).(bool); ok && showCorrupted {
		pa.SetShowCorrupted()
	}

	return fs.nzbFs.OpenFile(ctx, pa.String(), flag, perm)
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
