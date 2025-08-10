package virtualfs

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/sourcegraph/conc/pool"
	"github.com/spf13/afero"
)

type file struct {
	innerFile  afero.File
	remoteFile RemoteFile
	log        *slog.Logger
	ctx        context.Context
}

func OpenFile(
	ctx context.Context,
	name string,
	flag int,
	perm fs.FileMode,
	remoteFile RemoteFile,
) (*file, error) {
	log := slog.Default()
	f, err := os.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}

	return &file{
		innerFile:  f,
		log:        log,
		remoteFile: remoteFile,
	}, nil
}

func (f *file) Close() error {
	return f.innerFile.Close()
}

func (f *file) Name() string {
	return f.innerFile.Name()
}

func (f *file) Read(b []byte) (int, error) {
	return f.innerFile.Read(b)
}

func (f *file) ReadAt(b []byte, off int64) (int, error) {
	return f.innerFile.ReadAt(b, off)
}

func (f *file) Readdir(n int) ([]os.FileInfo, error) {
	infos, err := f.innerFile.Readdir(n)
	if err != nil {
		return nil, err
	}

	p := pool.New().WithMaxGoroutines(10).WithErrors()

	for i, info := range infos {
		if info.IsDir() {
			continue
		}

		name := info.Name()
		i := i
		p.Go(func() error {
			if info == nil {
				return nil
			}

			pathJoin := filepath.Join(f.innerFile.Name(), name)
			ok, s, err := f.remoteFile.StatToRemoteStat(pathJoin, info)
			if err != nil {
				infos[i] = nil
				return err
			}

			if ok {
				infos[i] = s
			}

			return nil
		})
	}

	if err := p.Wait(); err != nil {
		f.log.ErrorContext(f.ctx, "error reading remote directory", "error", err)

		// Remove nulls from infos
		var filteredInfos []os.FileInfo
		for _, info := range infos {
			if info != nil {
				filteredInfos = append(filteredInfos, info)
			}
		}

		return filteredInfos, nil
	}

	return infos, nil
}

func (f *file) Readdirnames(n int) ([]string, error) {
	return f.innerFile.Readdirnames(n)
}

func (f *file) Seek(offset int64, whence int) (int64, error) {
	return f.innerFile.Seek(offset, whence)
}

func (f *file) Stat() (fs.FileInfo, error) {
	return f.innerFile.Stat()
}

func (f *file) Sync() error {
	return f.innerFile.Sync()
}

func (f *file) Truncate(size int64) error {
	return f.innerFile.Truncate(size)
}

func (f *file) Write(b []byte) (int, error) {
	return f.innerFile.Write(b)
}

func (f *file) WriteAt(b []byte, off int64) (int, error) {
	return f.innerFile.WriteAt(b, off)
}

func (f *file) WriteString(s string) (int, error) {
	return f.innerFile.WriteString(s)
}
