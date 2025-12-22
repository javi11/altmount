package fuse

import (
	"context"
	"os"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/utils"
	"github.com/spf13/afero"
)

// ensure ContextAdapter implements afero.Fs
var _ afero.Fs = (*ContextAdapter)(nil)

// ContextAdapter wraps a context-aware filesystem to implement afero.Fs
// It uses context.Background() for all operations
type ContextAdapter struct {
	fs     *nzbfilesystem.NzbFilesystem
	config config.FuseConfig
}

// NewContextAdapter creates a new adapter
func NewContextAdapter(fs *nzbfilesystem.NzbFilesystem, cfg config.FuseConfig) *ContextAdapter {
	return &ContextAdapter{
		fs:     fs,
		config: cfg,
	}
}

func (c *ContextAdapter) Create(name string) (afero.File, error) {
	return c.fs.Create(name)
}

func (c *ContextAdapter) Mkdir(name string, perm os.FileMode) error {
	return c.fs.Mkdir(context.Background(), name, perm)
}

func (c *ContextAdapter) MkdirAll(name string, perm os.FileMode) error {
	return c.fs.MkdirAll(context.Background(), name, perm)
}

func (c *ContextAdapter) Open(name string) (afero.File, error) {
	ctx := context.Background()
	if c.config.MaxCacheSizeMB > 0 {
		ctx = context.WithValue(ctx, utils.MaxCacheSizeKey, c.config.MaxCacheSizeMB)
	}
	return c.fs.Open(ctx, name)
}

func (c *ContextAdapter) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	ctx := context.Background()
	if c.config.MaxCacheSizeMB > 0 {
		ctx = context.WithValue(ctx, utils.MaxCacheSizeKey, c.config.MaxCacheSizeMB)
	}
	return c.fs.OpenFile(ctx, name, flag, perm)
}

func (c *ContextAdapter) Remove(name string) error {
	return c.fs.Remove(context.Background(), name)
}

func (c *ContextAdapter) RemoveAll(name string) error {
	return c.fs.RemoveAll(context.Background(), name)
}

func (c *ContextAdapter) Rename(oldname, newname string) error {
	return c.fs.Rename(context.Background(), oldname, newname)
}

func (c *ContextAdapter) Stat(name string) (os.FileInfo, error) {
	return c.fs.Stat(context.Background(), name)
}

func (c *ContextAdapter) Name() string {
	return c.fs.Name()
}

func (c *ContextAdapter) Chmod(name string, mode os.FileMode) error {
	return c.fs.Chmod(name, mode)
}

func (c *ContextAdapter) Chown(name string, uid, gid int) error {
	return c.fs.Chown(name, uid, gid)
}

func (c *ContextAdapter) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return c.fs.Chtimes(name, atime, mtime)
}

// GetUnderlyingFs returns the underlying filesystem
func (c *ContextAdapter) GetUnderlyingFs() *nzbfilesystem.NzbFilesystem {
	return c.fs
}
