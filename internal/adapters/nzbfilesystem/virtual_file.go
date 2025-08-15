package nzbfilesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/altmount/internal/utils"
	"github.com/javi11/nntppool"
)

// VirtualFile represents a file backed by NZB data
type VirtualFile struct {
	name        string
	virtualFile *database.VirtualFile
	nzbFile     *database.NzbFile
	db          *database.DB
	args        utils.PathWithArgs
	position    int64 // Current read position in the file
	// NNTP and reading state
	cp             nntppool.UsenetConnectionPool
	reader         io.ReadCloser
	ctx            context.Context
	maxWorkers     int
	rcloneCipher   encryption.Cipher // For encryption/decryption
	headersCipher  encryption.Cipher // For headers encryption/decryption
	globalPassword string            // Global password fallback
	globalSalt     string            // Global salt fallback
	mu             sync.Mutex
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

// Stat returns file information
func (vf *VirtualFile) Stat() (fs.FileInfo, error) {
	return &VirtualFileInfo{
		name:    vf.virtualFile.Name,
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
	return ErrTruncateNotSupported
}

// Write is not supported for virtual files
func (vf *VirtualFile) Write(p []byte) (int, error) {
	return 0, ErrWriteNotSupported
}

// WriteAt is not supported for virtual files
func (vf *VirtualFile) WriteAt(p []byte, off int64) (int, error) {
	return 0, ErrWriteNotSupported
}

// WriteString is not supported for virtual files
func (vf *VirtualFile) WriteString(s string) (int, error) {
	return 0, ErrWriteNotSupported
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

// Read reads data from the virtual file using lazy reader creation with proper chunk continuation
func (vf *VirtualFile) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if vf.virtualFile == nil {
		return 0, ErrVirtualFileNotInit
	}

	if vf.virtualFile.IsDirectory {
		return 0, ErrCannotReadDirectory
	}

	if vf.nzbFile == nil {
		return 0, ErrNoNzbData
	}

	if vf.reader == nil {
		// Ensure we have a reader for the current position
		if err := vf.ensureReader(); err != nil {
			if errors.Is(err, io.EOF) {
				// If EOF, return 0 bytes read
				if vf.position == 0 {
					return 0, io.EOF // No data read at all
				}
				return 0, nil // EOF reached after some data read
			}

			return 0, err
		}
	}

	totalRead, err := vf.reader.Read(p)
	if err != nil {
		// Check if this is an ArticleNotFoundError from usenet reader
		if articleErr, ok := err.(*usenet.ArticleNotFoundError); ok {
			// Update database status based on bytes read
			vf.updateFileStatusFromError(articleErr)

			// Create appropriate error for upper layers
			if articleErr.BytesRead > 0 || totalRead > 0 {
				// Some content was read - return partial content error
				return totalRead, &PartialContentError{
					BytesRead:     articleErr.BytesRead,
					TotalExpected: vf.virtualFile.Size,
					UnderlyingErr: articleErr,
				}
			} else {
				// No content read - return corrupted file error
				return totalRead, &CorruptedFileError{
					TotalExpected: vf.virtualFile.Size,
					UnderlyingErr: articleErr,
				}
			}
		}
		// Any other error should be returned as-is
		return totalRead, err
	}

	// Update the current position after reading
	vf.position += int64(totalRead)

	return totalRead, nil
}

// ReadAt reads data at a specific offset
func (vf *VirtualFile) ReadAt(p []byte, off int64) (int, error) {
	return 0, os.ErrPermission
}

// Seek sets the file position and invalidates reader if position changes significantly
func (vf *VirtualFile) Seek(offset int64, whence int) (int64, error) {
	vf.mu.Lock()
	defer vf.mu.Unlock()
	var abs int64

	switch whence {
	case io.SeekStart: // Relative to the origin of the file
		abs = offset
	case io.SeekCurrent: // Relative to the current offset
		abs = vf.position + offset
	case io.SeekEnd: // Relative to the end
		abs = int64(vf.virtualFile.Size) + offset
	default:
		return 0, ErrInvalidWhence
	}

	if abs < 0 {
		return 0, ErrSeekNegative
	}

	if abs > int64(vf.virtualFile.Size) {
		return 0, ErrSeekTooFar
	}

	vf.position = abs
	return abs, nil
}

// ensureReaderForPosition creates or reuses a reader for the given position with smart chunking
// This implements lazy loading to avoid memory leaks from pre-caching entire files
func (vf *VirtualFile) ensureReader() error {
	if vf.nzbFile == nil {
		return ErrNoNzbData
	}

	if vf.cp == nil {
		return ErrNoUsenetPool
	}

	var isRarFile bool

	if isRarFile {
		// TODO handle
		return nil
	}

	start, end, _ := vf.getRequestRange()

	// Create reader for the calculated range
	if vf.nzbFile != nil && vf.nzbFile.Encryption != nil {
		// Wrap the usenet reader with rclone decryption
		decryptedReader, err := vf.wrapWithEncryption(start, end)
		if err != nil {
			return fmt.Errorf(ErrMsgFailedWrapEncryption, err)
		}

		vf.reader = decryptedReader
	} else {
		ur, err := vf.createUsenetReader(vf.ctx, start, end)
		if err != nil {
			return err
		}

		vf.reader = ur
	}

	return nil
}

// Readdir lists directory contents
func (vf *VirtualFile) Readdir(n int) ([]os.FileInfo, error) {
	if !vf.virtualFile.IsDirectory {
		return nil, ErrNotDirectory
	}

	// For root directory ("/"), list items with parent_id = NULL
	// For other directories, list items with parent_id = directory ID
	var parentID *int64
	if vf.virtualFile.ParentID == nil { // Root directories have parent_id = NULL
		parentID = nil // Root level items have parent_id = NULL
	} else {
		parentID = &vf.virtualFile.ID
	}

	children, err := vf.db.Repository.ListVirtualFilesByParentID(parentID)
	if err != nil {
		return nil, errors.Join(err, ErrFailedListDirectory)
	}

	var infos []os.FileInfo
	for _, child := range children {
		info := &VirtualFileInfo{
			name:    child.Name,
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

// updateFileStatusFromError updates the virtual file status in the database based on ArticleNotFoundError
func (vf *VirtualFile) updateFileStatusFromError(articleErr *usenet.ArticleNotFoundError) {
	if vf.db == nil || vf.virtualFile == nil {
		return // No database access or virtual file info
	}

	var status database.FileStatus
	if articleErr.BytesRead > 0 {
		// Some content was successfully read before error - mark as partial
		status = database.FileStatusPartial
	} else {
		// No content read - mark as corrupted/missing
		status = database.FileStatusCorrupted
	}

	// Update status in database - ignore errors as this is best-effort
	repo := database.NewRepository(vf.db.Connection())
	_ = repo.UpdateVirtualFileStatus(vf.virtualFile.ID, status)
}

// getRequestRange extracts and validates the HTTP Range header from the original request
// Returns the effective range to use for reader creation, considering file size limits
func (vf *VirtualFile) getRequestRange() (start, end int64, hasRange bool) {
	// Try to get range from HTTP request args
	rangeHeader, err := vf.args.Range()
	if err != nil || rangeHeader == nil {
		// No valid range header, return full file range
		return 0, vf.virtualFile.Size - 1, false
	}

	// Fix range header to ensure it's within file bounds
	fixedRange := utils.FixRangeHeader(rangeHeader, vf.virtualFile.Size)
	if fixedRange == nil {
		return 0, vf.virtualFile.Size - 1, false
	}

	// Ensure range is valid
	if fixedRange.Start < 0 {
		fixedRange.Start = 0
	}
	if fixedRange.End >= vf.virtualFile.Size {
		fixedRange.End = vf.virtualFile.Size - 1
	}
	if fixedRange.Start > fixedRange.End {
		return 0, vf.virtualFile.Size - 1, false
	}

	return fixedRange.Start, fixedRange.End, true
}

// createUsenetReader creates a new usenet reader for the specified range
func (vf *VirtualFile) createUsenetReader(ctx context.Context, start, end int64) (io.ReadCloser, error) {
	if vf.nzbFile == nil || vf.nzbFile.SegmentsData == nil {
		return nil, ErrNoNzbData
	}

	loader := newSegmentDataLoader(vf.nzbFile.SegmentsData)

	rg := usenet.GetSegmentsInRange(start, end, loader)
	return usenet.NewUsenetReader(ctx, vf.cp, rg, vf.maxWorkers)
}
