//go:build linux

package hanwen

import (
	"context"
	"os"

	"github.com/spf13/afero"
)

// nzbFS is the subset of *nzbfilesystem.NzbFilesystem used by the FUSE nodes.
// Depending on the interface instead of the concrete type keeps Dir/File
// testable without standing up the full metadata stack.
type nzbFS interface {
	Open(ctx context.Context, name string) (afero.File, error)
	Stat(ctx context.Context, name string) (os.FileInfo, error)
	Mkdir(ctx context.Context, name string, perm os.FileMode) error
	Rename(ctx context.Context, oldName, newName string) error
	Remove(ctx context.Context, name string) error
}
