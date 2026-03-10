package webdav

import (
	"context"
	"io"
	"os"
)

// FileSystem provides virtual filesystem access for WebDAV.
type FileSystem interface {
	Mkdir(ctx context.Context, name string, perm os.FileMode) error
	OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (File, error)
	RemoveAll(ctx context.Context, name string) error
	Rename(ctx context.Context, oldName, newName string) error
	Stat(ctx context.Context, name string) (os.FileInfo, error)
}

// File provides access to a single file or directory.
type File interface {
	io.ReadSeekCloser
	Write(p []byte) (n int, err error)
	Readdir(count int) ([]os.FileInfo, error)
	Stat() (os.FileInfo, error)
}
