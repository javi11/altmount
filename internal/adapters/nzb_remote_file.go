package adapters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/altmount/internal/utils"
	"github.com/javi11/nntppool"
	"github.com/javi11/nzbparser"
	"github.com/spf13/afero"
)

// NzbRemoteFile implements the RemoteFile interface for NZB-backed virtual files
type NzbRemoteFile struct {
	db                 *database.DB
	cp                 nntppool.UsenetConnectionPool
	maxDownloadWorkers int
}

// NewNzbRemoteFile creates a new NZB remote file handler
func NewNzbRemoteFile(db *database.DB, cp nntppool.UsenetConnectionPool, maxDownloadWorkers int) *NzbRemoteFile {
	return &NzbRemoteFile{
		db:                 db,
		cp:                 cp,
		maxDownloadWorkers: maxDownloadWorkers,
	}
}

// normalizePath normalizes file paths for consistent database lookups
// Removes trailing slashes except for root path "/"
func normalizePath(path string) string {
	// Handle empty path
	if path == "" {
		return "/"
	}

	// Handle root path - keep as is
	if path == "/" {
		return path
	}

	// Remove trailing slashes for all other paths
	return strings.TrimRight(path, "/")
}

// OpenFile opens a virtual file backed by NZB data
func (nrf *NzbRemoteFile) OpenFile(ctx context.Context, name string, r utils.PathWithArgs) (bool, afero.File, error) {
	// Normalize the path to handle trailing slashes consistently
	normalizedName := normalizePath(name)

	// Check if this is a virtual file in our database
	vf, nzb, err := nrf.db.Repository.GetVirtualFileWithNzb(normalizedName)
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
		nzbFile:     nzb, // Can be nil for system directories like root
		db:          nrf.db,
		args:        r,
		cp:          nrf.cp,
		ctx:         ctx,
		maxWorkers:  nrf.maxDownloadWorkers,
	}

	return true, virtualFile, nil
}

// RemoveFile removes a virtual file from the database
func (nrf *NzbRemoteFile) RemoveFile(ctx context.Context, fileName string) (bool, error) {
	// Normalize the path to handle trailing slashes consistently
	normalizedName := normalizePath(fileName)

	// Prevent removal of root directory
	if normalizedName == "/" {
		return false, fmt.Errorf("cannot remove root directory")
	}

	// Check if this is a virtual file
	vf, err := nrf.db.Repository.GetVirtualFileByPath(normalizedName)
	if err != nil {
		return false, fmt.Errorf("failed to query virtual file: %w", err)
	}

	if vf == nil {
		// File not found in database
		return false, nil
	}

	// Use transaction to ensure atomicity
	err = nrf.db.Repository.WithTransaction(func(txRepo *database.Repository) error {
		// Delete the virtual file (CASCADE will handle all descendants automatically)
		// The foreign key constraint ON DELETE CASCADE will recursively remove all children
		if err := txRepo.DeleteVirtualFile(vf.ID); err != nil {
			return fmt.Errorf("failed to delete virtual file: %w", err)
		}

		return nil
	})

	if err != nil {
		return false, err
	}

	return true, nil
}

// RenameFile renames a virtual file in the database
func (nrf *NzbRemoteFile) RenameFile(ctx context.Context, fileName, newFileName string) (bool, error) {
	// Normalize paths to handle trailing slashes consistently
	normalizedOldName := normalizePath(fileName)
	normalizedNewName := normalizePath(newFileName)

	// Prevent renaming the root directory
	if normalizedOldName == "/" {
		return false, fmt.Errorf("cannot rename root directory")
	}

	// Prevent renaming to root directory
	if normalizedNewName == "/" {
		return false, fmt.Errorf("cannot rename to root directory")
	}

	// Check if source file exists
	vf, err := nrf.db.Repository.GetVirtualFileByPath(normalizedOldName)
	if err != nil {
		return false, fmt.Errorf("failed to query virtual file: %w", err)
	}

	if vf == nil {
		// File not found in database
		return false, nil
	}

	// Parse new path to get parent directory and filename
	newDir := filepath.Dir(normalizedNewName)
	newFilename := filepath.Base(normalizedNewName)

	// Ensure new directory path uses forward slashes
	newDir = strings.ReplaceAll(newDir, string(filepath.Separator), "/")
	if newDir == "." {
		newDir = "/"
	}

	// Use transaction to ensure atomicity
	err = nrf.db.Repository.WithTransaction(func(txRepo *database.Repository) error {
		// Check if destination already exists
		existing, err := txRepo.GetVirtualFileByPath(normalizedNewName)
		if err != nil {
			return fmt.Errorf("failed to check destination: %w", err)
		}

		if existing != nil {
			return fmt.Errorf("destination already exists")
		}

		// Determine new parent ID
		var newParentID *int64
		if newDir == "/" {
			// Moving to root - parent_id should be NULL
			newParentID = nil
		} else {
			// Find the parent directory
			parentDir, err := txRepo.GetVirtualFileByPath(newDir)
			if err != nil {
				return fmt.Errorf("failed to find parent directory: %w", err)
			}

			if parentDir == nil {
				return fmt.Errorf("parent directory does not exist: %s", newDir)
			}

			if !parentDir.IsDirectory {
				return fmt.Errorf("parent is not a directory: %s", newDir)
			}

			newParentID = &parentDir.ID
		}

		// Update the virtual file
		if err := txRepo.MoveFile(vf.ID, newParentID, normalizedNewName); err != nil {
			return fmt.Errorf("failed to move file: %w", err)
		}

		// Update the filename if it changed
		if newFilename != vf.Filename {
			if err := txRepo.UpdateVirtualFileFilename(vf.ID, newFilename); err != nil {
				return fmt.Errorf("failed to update filename: %w", err)
			}
		}

		// If this is a directory, we need to update all descendant paths
		if vf.IsDirectory {
			descendants, err := txRepo.GetDescendants(vf.ID)
			if err != nil {
				return fmt.Errorf("failed to get descendants: %w", err)
			}

			for _, desc := range descendants {
				// Update descendant paths by replacing the old prefix with the new prefix
				oldPrefix := normalizedOldName
				if !strings.HasSuffix(oldPrefix, "/") {
					oldPrefix += "/"
				}

				newPrefix := normalizedNewName
				if !strings.HasSuffix(newPrefix, "/") {
					newPrefix += "/"
				}

				if strings.HasPrefix(desc.VirtualPath, oldPrefix) {
					newDescPath := strings.Replace(desc.VirtualPath, oldPrefix, newPrefix, 1)

					// Update descendant path
					if err := txRepo.UpdateVirtualFilePath(desc.ID, newDescPath); err != nil {
						return fmt.Errorf("failed to update descendant path: %w", err)
					}
				}
			}
		}

		return nil
	})

	if err != nil {
		return false, err
	}

	return true, nil
}

// Stat returns file information for a virtual file
func (nrf *NzbRemoteFile) Stat(fileName string) (bool, fs.FileInfo, error) {
	// Normalize the path to handle trailing slashes consistently
	normalizedName := normalizePath(fileName)

	// Check if this is a virtual file
	vf, err := nrf.db.Repository.GetVirtualFileByPath(normalizedName)
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

	// NNTP and reading state
	cp         nntppool.UsenetConnectionPool
	reader     io.ReadCloser
	ctx        context.Context
	maxWorkers int
	mu         sync.Mutex
}

// Close closes the virtual file
func (vf *VirtualFile) Close() error {
	vf.mu.Lock()
	defer vf.mu.Unlock()
	if vf.reader != nil {
		_ = vf.reader.Close()
		vf.reader = nil
	}
	return nil
}

// Name returns the file name
func (vf *VirtualFile) Name() string {
	return vf.name
}

// dbSegmentLoader adapts DB segments to the usenet.SegmentLoader interface
type dbSegmentLoader struct {
	segs database.NzbSegments
}

func (l dbSegmentLoader) GetSegment(index int) (segment nzbparser.NzbSegment, groups []string, ok bool) {
	if index < 0 || index >= len(l.segs) {
		return nzbparser.NzbSegment{}, nil, false
	}
	s := l.segs[index]
	return nzbparser.NzbSegment{Number: s.Number, Bytes: int(s.Bytes), ID: s.MessageID}, s.Groups, true
}

func (vf *VirtualFile) ensureReader(start int64) error {
	// If an existing reader is open for a different position, close and recreate
	if vf.reader != nil && start != vf.position {
		_ = vf.reader.Close()
		vf.reader = nil
	}
	if vf.reader != nil {
		return nil
	}
	if vf.nzbFile == nil {
		return fmt.Errorf("no NZB data available for file")
	}
	if vf.cp == nil {
		return fmt.Errorf("usenet connection pool not configured")
	}
	if start < 0 {
		start = 0
	}
	if start >= vf.virtualFile.Size {
		return io.EOF
	}
	end := vf.virtualFile.Size - 1 // inclusive
	loader := dbSegmentLoader{segs: vf.nzbFile.SegmentsData}
	// If we have a stored segment size, use it to compute ranges
	hasFixedSize := vf.nzbFile.SegmentSize > 0
	segSize := vf.nzbFile.SegmentSize
	rg := usenet.GetSegmentsInRange(start, end, loader, hasFixedSize, segSize)
	ur, err := usenet.NewUsenetReader(vf.ctx, vf.cp, rg, vf.maxWorkers)
	if err != nil {
		return err
	}
	vf.reader = ur
	// align internal position with requested start
	vf.position = start
	return nil
}

// Read reads data from the virtual file
func (vf *VirtualFile) Read(p []byte) (int, error) {
	vf.mu.Lock()
	defer vf.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	if vf.virtualFile == nil {
		return 0, fmt.Errorf("virtual file not initialized")
	}
	if vf.virtualFile.IsDirectory {
		return 0, fmt.Errorf("cannot read from directory")
	}
	if vf.nzbFile == nil {
		return 0, fmt.Errorf("no NZB data available for file")
	}
	// Ensure reader from current position
	if err := vf.ensureReader(vf.position); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, io.EOF
		}
		return 0, err
	}
	n, err := vf.reader.Read(p)
	vf.position += int64(n)
	return n, err
}

// ReadAt reads data at a specific offset
func (vf *VirtualFile) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if vf.virtualFile.IsDirectory {
		return 0, fmt.Errorf("cannot read from directory")
	}
	if vf.nzbFile == nil {
		return 0, fmt.Errorf("no NZB data available for file")
	}
	if off < 0 {
		return 0, fmt.Errorf("negative offset")
	}
	if off >= vf.virtualFile.Size {
		return 0, io.EOF
	}
	// Limit read length to available bytes
	maxLen := int64(len(p))
	remain := vf.virtualFile.Size - off
	if maxLen > remain {
		maxLen = remain
	}
	end := off + maxLen - 1 // inclusive
	loader := dbSegmentLoader{segs: vf.nzbFile.SegmentsData}
	// If we have a stored segment size, use it to compute ranges
	hasFixedSize := vf.nzbFile.SegmentSize > 0
	segSize := vf.nzbFile.SegmentSize
	rg := usenet.GetSegmentsInRange(off, end, loader, hasFixedSize, segSize)
	ur, err := usenet.NewUsenetReader(vf.ctx, vf.cp, rg, vf.maxWorkers)
	if err != nil {
		return 0, err
	}
	defer ur.Close()
	buf := p[:maxLen]
	n := 0
	for n < len(buf) {
		nn, rerr := ur.Read(buf[n:])
		n += nn
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			return n, rerr
		}
	}
	if int64(n) < int64(len(p)) {
		return n, io.EOF
	}
	return n, nil
}

// Readdir lists directory contents
func (vf *VirtualFile) Readdir(n int) ([]os.FileInfo, error) {
	if !vf.virtualFile.IsDirectory {
		return nil, fmt.Errorf("not a directory")
	}

	// For root directory ("/"), list items with parent_id = NULL
	// For other directories, list items with parent_id = directory ID
	var parentID *int64
	if vf.virtualFile.VirtualPath == "/" {
		parentID = nil // Root level items have parent_id = NULL
	} else {
		parentID = &vf.virtualFile.ID
	}

	children, err := vf.db.Repository.ListVirtualFilesByParentID(parentID)
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

		// If n > 0, limit the results
		if n > 0 && len(infos) >= n {
			break
		}
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
	vf.mu.Lock()
	defer vf.mu.Unlock()
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
	// Reset current reader so next Read starts from new position
	if vf.reader != nil {
		_ = vf.reader.Close()
		vf.reader = nil
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
