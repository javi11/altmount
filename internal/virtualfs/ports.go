package virtualfs

import (
	"context"
	"io/fs"

	"github.com/javi11/altmount/internal/utils"
	"github.com/spf13/afero"
)

type RemoteFile interface {
	OpenFile(ctx context.Context, name string, r utils.PathWithArgs) (bool, afero.File, error)
	RemoveFile(ctx context.Context, fileName string) (bool, error)
	StatToRemoteStat(path string, stat fs.FileInfo) (bool, fs.FileInfo, error)
	RenameFile(ctx context.Context, fileName string, newFileName string) (bool, error)
	Stat(fileName string) (bool, fs.FileInfo, error)
}
