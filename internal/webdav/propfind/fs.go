package propfind

import (
	"context"
	"io"
	"os"
)

// FS is the minimal filesystem interface needed for PROPFIND operations.
type FS interface {
	OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (FSFile, error)
	Stat(ctx context.Context, name string) (os.FileInfo, error)
}

// FSFile is the minimal file interface needed for PROPFIND directory traversal.
type FSFile interface {
	io.Closer
	Readdir(count int) ([]os.FileInfo, error)
}
