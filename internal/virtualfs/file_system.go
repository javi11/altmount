package virtualfs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/slogutil"
	"github.com/javi11/altmount/internal/utils"
	"github.com/javi11/altmount/pkg/rclonecli"
	"github.com/spf13/afero"
)

type virtualFS struct {
	rootPath   string
	log        *slog.Logger
	rcloneCli  rclonecli.RcloneRcClient
	remoteFile RemoteFile
}

func New(
	rootPath string,
	remoteFile RemoteFile,
	rcloneCli rclonecli.RcloneRcClient,
) afero.Fs {
	log := slog.Default()

	return &virtualFS{
		rootPath:   rootPath,
		log:        log,
		remoteFile: remoteFile,
		rcloneCli:  rcloneCli,
	}
}

func (vfs *virtualFS) Mkdir(name string, perm os.FileMode) error {
	if name = vfs.resolve(name); name == "" {
		return os.ErrNotExist
	}

	err := os.Mkdir(name, perm)
	if err != nil {
		return err
	}

	vfs.refreshRcloneCache(context.Background(), name)

	return nil
}

func (vfs *virtualFS) MkdirAll(name string, perm os.FileMode) error {
	if name = vfs.resolve(name); name == "" {
		return os.ErrNotExist
	}

	err := os.MkdirAll(name, perm)
	if err != nil {
		return err
	}

	vfs.refreshRcloneCache(context.Background(), name)

	return nil
}

func (vfs *virtualFS) Name() string {
	return "virtualFS"
}

func (vfs *virtualFS) Open(name string) (afero.File, error) {
	ctx := slogutil.With(context.Background(), "file_path", name)

	pr, err := utils.NewPathWithArgsFromString(name)
	if err != nil {
		vfs.log.ErrorContext(ctx, "Failed to parse path with args", "err", err)
		return nil, io.ErrUnexpectedEOF
	}
	name = pr.Path

	if name = vfs.resolve(name); name == "" {
		return nil, os.ErrNotExist
	}

	ok, f, err := vfs.remoteFile.OpenFile(ctx, name, pr)
	if err != nil {
		return nil, err
	}

	if ok {
		// Return the file in case it was found in the remote
		return f, nil
	}

	return OpenFile(ctx, name, os.O_RDONLY, 0, vfs.remoteFile)
}

func (vfs *virtualFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	ctx := slogutil.With(context.Background(), "file_path", name)

	isWriteMode := flag == os.O_RDWR|os.O_CREATE|os.O_TRUNC

	if isWriteMode {
		slog.InfoContext(ctx, "Opening file for writing is not permitted", "name", name)
		return nil, os.ErrPermission
	}

	pr, err := utils.NewPathWithArgsFromString(name)
	if err != nil {
		vfs.log.ErrorContext(ctx, "Failed to parse path with args", "err", err)
		return nil, io.ErrUnexpectedEOF
	}

	name = pr.Path

	if name = vfs.resolve(name); name == "" {
		return nil, os.ErrNotExist
	}

	ok, f, err := vfs.remoteFile.OpenFile(ctx, name, pr)
	if err != nil {
		vfs.refreshRcloneCache(ctx, name)
		return nil, errors.Join(os.ErrNotExist, err)
	}

	if ok {
		// Return the file in case it was found in the remote
		return f, nil
	}

	return OpenFile(ctx, name, flag, perm, vfs.remoteFile)
}

func (vfs *virtualFS) RemoveAll(name string) error {
	if name = vfs.resolve(name); name == "" {
		return os.ErrNotExist
	}

	ctx := slogutil.With(context.Background(), "file_path", name)
	ok, err := vfs.remoteFile.RemoveFile(ctx, name)
	if err != nil {
		return err
	}

	if ok {
		return nil
	}

	defer vfs.refreshRcloneCache(ctx, name)

	if name == filepath.Clean(vfs.rootPath) {
		// Prohibit removing the virtual root directory.
		return os.ErrInvalid
	}

	err = os.RemoveAll(name)
	if err != nil {
		return err
	}

	return nil
}

func (vfs *virtualFS) Rename(oldName, newName string) error {
	if oldName = vfs.resolve(oldName); oldName == "" {
		return os.ErrNotExist
	}
	if newName = vfs.resolve(newName); newName == "" {
		return os.ErrNotExist
	}

	ctx := slogutil.With(context.Background(), "file_path", oldName, "new_file_path", newName)
	ok, err := vfs.remoteFile.RenameFile(ctx, oldName, newName)
	if err != nil {
		return errors.Join(err, os.ErrPermission)
	}

	defer vfs.refreshRcloneCache(ctx, newName)

	if ok {
		return nil
	}

	if root := filepath.Clean(vfs.rootPath); root == oldName || root == newName {
		// Prohibit renaming from or to the virtual root directory.
		return os.ErrInvalid
	}

	err = os.Rename(oldName, newName)
	if err != nil {
		return err
	}

	return nil
}

func (vfs *virtualFS) Stat(name string) (fs.FileInfo, error) {
	if name = vfs.resolve(name); name == "" {
		return nil, os.ErrNotExist
	}

	stat, e := os.Stat(name)
	if e != nil {
		if !os.IsNotExist(e) {
			return nil, e
		}
	}

	if stat != nil && stat.IsDir() {
		return stat, nil
	}

	ok, s, err := vfs.remoteFile.Stat(name)
	if err != nil {
		return nil, err
	}

	if ok {
		// Return the remote file info if it exists
		return s, nil
	}

	return stat, e
}

func (vfs *virtualFS) Chmod(name string, mode os.FileMode) error {
	if name = vfs.resolve(name); name == "" {
		return os.ErrNotExist
	}

	err := os.Chmod(name, mode)
	if err != nil {
		return err
	}

	vfs.refreshRcloneCache(context.Background(), name)

	return nil
}

func (vfs *virtualFS) Chown(name string, uid, gid int) error {
	if name = vfs.resolve(name); name == "" {
		return os.ErrNotExist
	}

	err := os.Chown(name, uid, gid)
	if err != nil {
		return err
	}

	vfs.refreshRcloneCache(context.Background(), name)

	return nil
}

func (vfs *virtualFS) Chtimes(name string, atime, mtime time.Time) error {
	if name = vfs.resolve(name); name == "" {
		return os.ErrNotExist
	}

	err := os.Chtimes(name, atime, mtime)
	if err != nil {
		return err
	}

	vfs.refreshRcloneCache(context.Background(), name)

	return nil
}

func (fs *virtualFS) Remove(name string) error {
	if name = fs.resolve(name); name == "" {
		return os.ErrNotExist
	}

	ctx := slogutil.With(context.Background(), "file_path", name)
	ok, err := fs.remoteFile.RemoveFile(ctx, name)
	if err != nil {
		return err
	}

	defer fs.refreshRcloneCache(ctx, name)

	if ok {
		return nil
	}

	err = os.Remove(name)
	if err != nil {
		return err
	}

	return nil
}

func (vfs *virtualFS) Create(name string) (afero.File, error) {
	if name = vfs.resolve(name); name == "" {
		return nil, os.ErrNotExist
	}

	f, err := os.Create(name)
	if err != nil {
		return nil, err
	}

	vfs.refreshRcloneCache(context.Background(), name)

	return f, nil
}

func (vfs *virtualFS) resolve(name string) string {
	// This implementation is based on Dir.Open's code in the standard net/http package.
	if filepath.Separator != '/' && strings.ContainsRune(name, filepath.Separator) ||
		strings.Contains(name, "\x00") {
		return ""
	}
	dir := vfs.rootPath
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, filepath.FromSlash(utils.SlashClean(name)))
}

func (vfs *virtualFS) refreshRcloneCache(ctx context.Context, name string) {
	if vfs.rcloneCli != nil {
		mountDir := filepath.Dir(strings.Replace(name, vfs.rootPath, "", 1))
		if mountDir == "/" {
			mountDir = ""
		}
		err := vfs.rcloneCli.RefreshCache(ctx, mountDir, true, false)
		if err != nil {
			vfs.log.ErrorContext(ctx, "Failed to refresh cache", "err", err)
		}
	}
}
