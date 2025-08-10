package adapters

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/utils"
	"github.com/spf13/afero"
)

// NzbRemoteFile implements the RemoteFile interface for NZB-backed virtual files
type NzbRemoteFile struct {
	db *database.DB
}

// NewNzbRemoteFile creates a new NZB remote file handler
func NewNzbRemoteFile(db *database.DB) *NzbRemoteFile {
	return &NzbRemoteFile{
		db: db,
	}
}

// OpenFile opens a virtual file backed by NZB data
func (nrf *NzbRemoteFile) OpenFile(ctx context.Context, name string, r utils.PathWithArgs) (bool, afero.File, error) {
	// Check if this is a virtual file in our database
	vf, nzb, err := nrf.db.Repository.GetVirtualFileWithNzb(name)
	if err != nil {
		return false, nil, fmt.Errorf("failed to query virtual file: %w", err)
	}

	if vf == nil {
		// File not found in database
		return false, nil, nil
	}

	// Create a virtual file handle
	virtualFile := &VirtualFile{
		name:        name,
		virtualFile: vf,
		nzbFile:     nzb,
		db:          nrf.db,
		args:        r,
	}

	return true, virtualFile, nil
}

// RemoveFile removes a virtual file from the database
func (nrf *NzbRemoteFile) RemoveFile(ctx context.Context, fileName string) (bool, error) {
	// Check if this is a virtual file
	vf, err := nrf.db.Repository.GetVirtualFileByPath(fileName)
	if err != nil {
		return false, fmt.Errorf("failed to query virtual file: %w", err)
	}

	if vf == nil {
		// File not found in database
		return false, nil
	}

	// For now, we don't support removing individual virtual files
	// In the future, this could be extended to remove the NZB entry
	return false, fmt.Errorf("removing virtual files is not supported")
}

// StatToRemoteStat converts a local file stat to a remote stat if applicable
func (nrf *NzbRemoteFile) StatToRemoteStat(path string, stat fs.FileInfo) (bool, fs.FileInfo, error) {
	// Check if this path corresponds to a virtual file
	vf, err := nrf.db.Repository.GetVirtualFileByPath(path)
	if err != nil {
		return false, nil, fmt.Errorf("failed to query virtual file: %w", err)
	}

	if vf == nil {
		// Not a virtual file, return original stat
		return false, stat, nil
	}

	// Create virtual file info
	virtualStat := &VirtualFileInfo{
		name:    vf.Filename,
		size:    vf.Size,
		modTime: vf.CreatedAt,
		isDir:   vf.IsDirectory,
	}

	return true, virtualStat, nil
}

// RenameFile renames a virtual file in the database
func (nrf *NzbRemoteFile) RenameFile(ctx context.Context, fileName, newFileName string) (bool, error) {
	// Check if this is a virtual file
	vf, err := nrf.db.Repository.GetVirtualFileByPath(fileName)
	if err != nil {
		return false, fmt.Errorf("failed to query virtual file: %w", err)
	}

	if vf == nil {
		// File not found in database
		return false, nil
	}

	// For now, we don't support renaming virtual files
	// This could be extended in the future
	return false, fmt.Errorf("renaming virtual files is not supported")
}

// Stat returns file information for a virtual file
func (nrf *NzbRemoteFile) Stat(fileName string) (bool, fs.FileInfo, error) {
	// Check if this is a virtual file
	vf, err := nrf.db.Repository.GetVirtualFileByPath(fileName)
	if err != nil {
		return false, nil, fmt.Errorf("failed to query virtual file: %w", err)
	}

	if vf == nil {
		// File not found in database
		return false, nil, nil
	}

	// Create virtual file info
	virtualStat := &VirtualFileInfo{
		name:    vf.Filename,
		size:    vf.Size,
		modTime: vf.CreatedAt,
		isDir:   vf.IsDirectory,
	}

	return true, virtualStat, nil
}

// VirtualFile represents a file backed by NZB data
type VirtualFile struct {
	name        string
	virtualFile *database.VirtualFile
	nzbFile     *database.NzbFile
	db          *database.DB
	args        utils.PathWithArgs
	position    int64
}

// Close closes the virtual file
func (vf *VirtualFile) Close() error {
	// Nothing to close for virtual files
	return nil
}

// Name returns the file name
func (vf *VirtualFile) Name() string {
	return vf.name
}

// Read reads data from the virtual file
func (vf *VirtualFile) Read(p []byte) (int, error) {
	// TODO: Implement reading from NZB segments
	// This would involve:
	// 1. Determining which segments contain the data at the current position
	// 2. Downloading the necessary segments from usenet
	// 3. Extracting the relevant data
	// 4. Handling decryption if needed
	
	return 0, fmt.Errorf("reading from virtual files not yet implemented")
}

// ReadAt reads data at a specific offset
func (vf *VirtualFile) ReadAt(p []byte, off int64) (int, error) {
	// TODO: Implement random access reading
	return 0, fmt.Errorf("random access reading from virtual files not yet implemented")
}

// Readdir is not applicable for files
func (vf *VirtualFile) Readdir(n int) ([]os.FileInfo, error) {
	if !vf.virtualFile.IsDirectory {
		return nil, fmt.Errorf("not a directory")
	}

	// List virtual files in this directory
	children, err := vf.db.Repository.ListVirtualFilesByParentPath(vf.virtualFile.VirtualPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory contents: %w", err)
	}

	var infos []os.FileInfo
	for _, child := range children {
		info := &VirtualFileInfo{
			name:    child.Filename,
			size:    child.Size,
			modTime: child.CreatedAt,
			isDir:   child.IsDirectory,
		}
		infos = append(infos, info)
	}

	return infos, nil
}

// Readdirnames returns directory entry names
func (vf *VirtualFile) Readdirnames(n int) ([]string, error) {
	infos, err := vf.Readdir(n)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.Name()
	}

	return names, nil
}

// Seek sets the file position
func (vf *VirtualFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case 0: // SEEK_SET
		vf.position = offset
	case 1: // SEEK_CUR
		vf.position += offset
	case 2: // SEEK_END
		vf.position = vf.virtualFile.Size + offset
	default:
		return 0, fmt.Errorf("invalid whence value")
	}

	if vf.position < 0 {
		vf.position = 0
	}

	return vf.position, nil
}

// Stat returns file information
func (vf *VirtualFile) Stat() (fs.FileInfo, error) {
	return &VirtualFileInfo{
		name:    vf.virtualFile.Filename,
		size:    vf.virtualFile.Size,
		modTime: vf.virtualFile.CreatedAt,
		isDir:   vf.virtualFile.IsDirectory,
	}, nil
}

// Sync is not applicable for virtual files
func (vf *VirtualFile) Sync() error {
	return nil
}

// Truncate is not supported for virtual files
func (vf *VirtualFile) Truncate(size int64) error {
	return fmt.Errorf("truncate not supported for virtual files")
}

// Write is not supported for virtual files
func (vf *VirtualFile) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("write not supported for virtual files")
}

// WriteAt is not supported for virtual files
func (vf *VirtualFile) WriteAt(p []byte, off int64) (int, error) {
	return 0, fmt.Errorf("write not supported for virtual files")
}

// WriteString is not supported for virtual files
func (vf *VirtualFile) WriteString(s string) (int, error) {
	return 0, fmt.Errorf("write not supported for virtual files")
}

// VirtualFileInfo implements fs.FileInfo for virtual files
type VirtualFileInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool
}

// Name returns the file name
func (vfi *VirtualFileInfo) Name() string {
	return vfi.name
}

// Size returns the file size
func (vfi *VirtualFileInfo) Size() int64 {
	return vfi.size
}

// Mode returns the file mode
func (vfi *VirtualFileInfo) Mode() fs.FileMode {
	if vfi.isDir {
		return fs.ModeDir | 0755
	}
	return 0644
}

// ModTime returns the modification time
func (vfi *VirtualFileInfo) ModTime() time.Time {
	return vfi.modTime
}

// IsDir returns whether this is a directory
func (vfi *VirtualFileInfo) IsDir() bool {
	return vfi.isDir
}

// Sys returns the underlying system interface (not used)
func (vfi *VirtualFileInfo) Sys() interface{} {
	return nil
}