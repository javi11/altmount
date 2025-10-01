package webdav

import (
	"context"
	"log/slog"
	"os"

	"github.com/javi11/altmount/internal/utils"
	"github.com/spf13/afero"
	"golang.org/x/net/webdav"
)

type fileSystem struct {
	afero.Fs
}

func aferoToWebdavFS(vfs afero.Fs) webdav.FileSystem {
	return &fileSystem{
		Fs: vfs,
	}
}

func (fs *fileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return fs.Fs.Mkdir(name, perm)
}

func (fs *fileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	pa := utils.NewPathWithArgs(name)
	r := ctx.Value(utils.RangeKey).(string)
	if r != "" {
		pa.SetRange(r)
	}

	s := ctx.Value(utils.ContentLengthKey).(string)
	if s != "" {
		pa.SetFileSize(s)
	}

	isCopy := ctx.Value(utils.IsCopy).(bool)
	if isCopy {
		pa.SetIsCopy()
	}

	origin := ctx.Value(utils.Origin).(string)
	if origin != "" {
		pa.SetOrigin(origin)
	}

	showCorrupted := ctx.Value(utils.ShowCorrupted).(bool)
	if showCorrupted {
		pa.SetShowCorrupted()
	}

	return fs.Fs.OpenFile(pa.String(), flag, perm)
}

func (fs *fileSystem) RemoveAll(ctx context.Context, name string) error {
	return fs.Fs.RemoveAll(name)
}

func (fs *fileSystem) Rename(ctx context.Context, oldName, newName string) error {
	// Add logging to understand when MOVE operations trigger renames
	slog.InfoContext(ctx, "WebDAV filesystem Rename called",
		"oldName", oldName,
		"newName", newName)
	return fs.Fs.Rename(oldName, newName)
}

func (fs *fileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return fs.Fs.Stat(name)
}
