package nzbfilesystem

import (
	"context"
	"io/fs"
	"os"
	"time"

	"github.com/javi11/altmount/internal/utils"
	"github.com/spf13/afero"
)

// NzbFilesystem implements afero.Fs interface directly using the metadata service
type NzbFilesystem struct {
	remoteFile *MetadataRemoteFile
}

// NewNzbFilesystem creates a new filesystem backed directly by metadata
func NewNzbFilesystem(remoteFile *MetadataRemoteFile) afero.Fs {
	return &NzbFilesystem{
		remoteFile: remoteFile,
	}
}

// Name returns the filesystem name
func (nfs *NzbFilesystem) Name() string {
	return "NzbFilesystem"
}

// Open opens a file for reading
func (nfs *NzbFilesystem) Open(name string) (afero.File, error) {
	ctx := context.Background()

	// Parse path with args
	pr, err := utils.NewPathWithArgsFromString(name)
	if err != nil {
		return nil, err
	}

	// Try to open with NZB remote file
	ok, file, err := nfs.remoteFile.OpenFile(ctx, pr.Path, pr)
	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, os.ErrNotExist
	}

	return file, nil
}

// OpenFile opens a file with specified flags and permissions
func (nfs *NzbFilesystem) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	// Only allow read operations
	if flag != os.O_RDONLY {
		return nil, os.ErrPermission
	}

	return nfs.Open(name)
}

// Stat returns file information
func (nfs *NzbFilesystem) Stat(name string) (fs.FileInfo, error) {
	ok, info, err := nfs.remoteFile.Stat(name)
	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, os.ErrNotExist
	}

	return info, nil
}

// Remove removes a file (not supported)
func (nfs *NzbFilesystem) Remove(name string) error {
	ctx := context.Background()
	defer func() {
		_ = nfs.remoteFile.healthRepository.DeleteHealthRecord(name)
	}()

	ok, err := nfs.remoteFile.RemoveFile(ctx, name)
	if err != nil {
		return err
	}

	if !ok {
		return os.ErrNotExist
	}

	return nil
}

// RemoveAll removes a file and any children (not supported)
func (nfs *NzbFilesystem) RemoveAll(name string) error {
	return nfs.Remove(name)
}

// Rename renames a file (not supported)
func (nfs *NzbFilesystem) Rename(oldName, newName string) error {
	ctx := context.Background()
	ok, err := nfs.remoteFile.RenameFile(ctx, oldName, newName)
	if err != nil {
		return err
	}

	if !ok {
		return os.ErrNotExist
	}

	return nil
}

// Create creates a new file (not supported - read-only filesystem)
func (nfs *NzbFilesystem) Create(name string) (afero.File, error) {
	return nil, os.ErrPermission
}

// Mkdir creates a directory (not supported - read-only filesystem)
func (nfs *NzbFilesystem) Mkdir(name string, perm os.FileMode) error {
	return os.ErrPermission
}

// MkdirAll creates a directory and all parent directories (not supported)
func (nfs *NzbFilesystem) MkdirAll(name string, perm os.FileMode) error {
	return os.ErrPermission
}

// Chmod changes file permissions (not supported)
func (nfs *NzbFilesystem) Chmod(name string, mode os.FileMode) error {
	return os.ErrPermission
}

// Chown changes file ownership (not supported)
func (nfs *NzbFilesystem) Chown(name string, uid, gid int) error {
	return os.ErrPermission
}

// Chtimes changes file times (not supported)
func (nfs *NzbFilesystem) Chtimes(name string, atime, mtime time.Time) error {
	return os.ErrPermission
}

// GetRemoteFile returns the underlying MetadataRemoteFile for configuration updates
func (nfs *NzbFilesystem) GetRemoteFile() *MetadataRemoteFile {
	return nfs.remoteFile
}
