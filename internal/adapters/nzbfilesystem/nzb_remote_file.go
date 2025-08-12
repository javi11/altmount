package nzbfilesystem

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
	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/encryption/rclone"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/altmount/internal/utils"
	"github.com/javi11/nntppool"
	"github.com/javi11/nzbparser"
	"github.com/spf13/afero"
)

var (
	ErrInvalidWhence = errors.New("seek: invalid whence")
	ErrSeekNegative  = errors.New("seek: negative position")
	ErrSeekTooFar    = errors.New("seek: too far")
)

// NzbRemoteFileConfig holds configuration for NzbRemoteFile
type NzbRemoteFileConfig struct {
	GlobalPassword string // Global password for .bin files
	GlobalSalt     string // Global salt for .bin files
}

// NzbRemoteFile implements the RemoteFile interface for NZB-backed virtual files
type NzbRemoteFile struct {
	db                 *database.DB
	cp                 nntppool.UsenetConnectionPool
	maxDownloadWorkers int
	rcloneCipher       encryption.Cipher // For rclone encryption/decryption
	globalPassword     string            // Global password fallback
	globalSalt         string            // Global salt fallback
}

// NewNzbRemoteFile creates a new NZB remote file handler with default config
func NewNzbRemoteFile(db *database.DB, cp nntppool.UsenetConnectionPool, maxDownloadWorkers int) *NzbRemoteFile {
	return NewNzbRemoteFileWithConfig(db, cp, maxDownloadWorkers, NzbRemoteFileConfig{})
}

// NewNzbRemoteFileWithConfig creates a new NZB remote file handler with configuration
func NewNzbRemoteFileWithConfig(db *database.DB, cp nntppool.UsenetConnectionPool, maxDownloadWorkers int, config NzbRemoteFileConfig) *NzbRemoteFile {
	// Initialize rclone cipher with global credentials for encrypted files
	rcloneConfig := &encryption.Config{
		RclonePassword: config.GlobalPassword, // Global password fallback
		RcloneSalt:     config.GlobalSalt,     // Global salt fallback
	}

	rcloneCipher, _ := rclone.NewRcloneCipher(rcloneConfig)

	return &NzbRemoteFile{
		db:                 db,
		cp:                 cp,
		maxDownloadWorkers: maxDownloadWorkers,
		rcloneCipher:       rcloneCipher,
		globalPassword:     config.GlobalPassword,
		globalSalt:         config.GlobalSalt,
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
		name:           name,
		virtualFile:    vf,
		nzbFile:        nzb, // Can be nil for system directories like root
		db:             nrf.db,
		args:           r,
		cp:             nrf.cp,
		ctx:            ctx,
		maxWorkers:     nrf.maxDownloadWorkers,
		rcloneCipher:   nrf.rcloneCipher,
		globalPassword: nrf.globalPassword,
		globalSalt:     nrf.globalSalt,
	}

	// Note: Reader is now created lazily on first read operation to avoid memory leaks

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
	cp             nntppool.UsenetConnectionPool
	reader         io.ReadCloser
	ctx            context.Context
	maxWorkers     int
	rcloneCipher   encryption.Cipher // For encryption/decryption
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

// ensureReaderForPosition creates or reuses a reader for the given position with smart chunking
// This implements lazy loading to avoid memory leaks from pre-caching entire files
func (vf *VirtualFile) ensureReaderForPosition(position int64) error {
	if vf.nzbFile == nil {
		return fmt.Errorf("no NZB data available for file")
	}

	if vf.cp == nil {
		return fmt.Errorf("usenet connection pool not configured")
	}

	if position < 0 {
		position = 0
	}

	if position >= vf.virtualFile.Size {
		return io.EOF
	}

	// Check if current reader can handle this position
	if vf.reader != nil {
		// If we have a reader and the position matches our current position, we're good
		if position == vf.position {
			return nil
		}
		// Position changed, close current reader
		_ = vf.reader.Close()
		vf.reader = nil
	}

	// Calculate smart range based on HTTP Range header and memory constraints
	start, end := vf.calculateSmartRange(position)

	// Create reader for the calculated range
	if vf.virtualFile.Encryption != nil && *vf.virtualFile.Encryption == "rclone" {
		// Wrap the usenet reader with rclone decryption
		decryptedReader, err := vf.wrapWithEncryption(start, end)
		if err != nil {
			return fmt.Errorf("failed to wrap reader with encryption: %w", err)
		}

		vf.reader = decryptedReader
	} else {
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
	}

	// Set position to the start of our new reader range
	vf.position = start
	return nil
}

// calculateSmartRange determines the optimal range for a reader based on position and constraints
func (vf *VirtualFile) calculateSmartRange(position int64) (start, end int64) {
	// Get HTTP range constraints if available
	rangeStart, rangeEnd, hasRange := vf.getRequestRange()

	// Define maximum chunk size to prevent memory explosion (100MB limit)
	const maxChunkSize = 100 * 1024 * 1024 // 100MB

	if hasRange {
		// Check if HTTP range is reasonable in size
		rangeSize := rangeEnd - rangeStart + 1

		if rangeSize <= maxChunkSize {
			// Range is reasonable, use it
			start = rangeStart
			end = rangeEnd
			if position < start {
				position = start
			}
			if position > end {
				position = end
			}
		} else {
			// Range is too large (likely end=-1 case), use chunking from current position
			// but respect the range boundaries
			start = position
			if start < rangeStart {
				start = rangeStart
			}

			// Use smart chunking within the HTTP range
			chunkSize := vf.getOptimalChunkSize()
			end = start + chunkSize - 1

			// Don't exceed the HTTP range end
			if end > rangeEnd {
				end = rangeEnd
			}
		}
	} else {
		// No HTTP range - use smart chunking to avoid memory leaks
		start = position
		chunkSize := vf.getOptimalChunkSize()
		end = start + chunkSize - 1

		if end >= vf.virtualFile.Size {
			end = vf.virtualFile.Size - 1
		}
	}

	// Final validation
	if start < 0 {
		start = 0
	}
	if end >= vf.virtualFile.Size {
		end = vf.virtualFile.Size - 1
	}
	if start > end {
		// Fallback to just the current position
		start = position
		end = position
	}

	return start, end
}

// getOptimalChunkSize returns the optimal chunk size based on file size
func (vf *VirtualFile) getOptimalChunkSize() int64 {
	fileSize := vf.virtualFile.Size

	switch {
	case fileSize < 10*1024*1024: // < 10MB
		// Small files: read entire file
		return fileSize
	case fileSize < 100*1024*1024: // < 100MB
		// Medium files: 10MB chunks
		return 10 * 1024 * 1024
	case fileSize < 1024*1024*1024: // < 1GB
		// Large files: 25MB chunks
		return 25 * 1024 * 1024
	default: // >= 1GB
		// Very large files: 50MB chunks
		return 50 * 1024 * 1024
	}
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

// wrapWithEncryption wraps a usenet reader with rclone decryption
func (vf *VirtualFile) wrapWithEncryption(start, end int64) (io.ReadCloser, error) {
	if vf.rcloneCipher == nil {
		return nil, fmt.Errorf("no cipher configured for encryption")
	}

	if vf.nzbFile == nil {
		return nil, fmt.Errorf("no NZB data available for encryption parameters")
	}

	// Get password and salt from NZB metadata, with global fallback
	var password, salt string

	if vf.nzbFile.RclonePassword != nil && *vf.nzbFile.RclonePassword != "" {
		password = *vf.nzbFile.RclonePassword
	} else {
		// Fallback to global password
		password = vf.globalPassword
	}

	if vf.nzbFile.RcloneSalt != nil && *vf.nzbFile.RcloneSalt != "" {
		salt = *vf.nzbFile.RcloneSalt
	} else {
		// Fallback to global salt
		salt = vf.globalSalt
	}

	decryptedReader, err := vf.rcloneCipher.Open(
		vf.ctx,
		&utils.RangeHeader{
			Start: start,
			End:   end,
		},
		vf.virtualFile.Size,
		password,
		salt,
		func(ctx context.Context, start, end int64) (io.ReadCloser, error) {
			// Create a new usenet reader for the specific range
			loader := dbSegmentLoader{segs: vf.nzbFile.SegmentsData}
			hasFixedSize := vf.nzbFile.SegmentSize > 0
			segSize := vf.nzbFile.SegmentSize
			rg := usenet.GetSegmentsInRange(start, end, loader, hasFixedSize, segSize)

			return usenet.NewUsenetReader(ctx, vf.cp, rg, vf.maxWorkers)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create decrypt reader: %w", err)
	}

	return decryptedReader, nil
}

// Read reads data from the virtual file using lazy reader creation
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

	// Ensure we have a reader for the current position
	if err := vf.ensureReaderForPosition(vf.position); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, io.EOF
		}
		return 0, err
	}

	n, err := vf.reader.Read(p)
	vf.position += int64(n)

	// If we hit EOF but read some data, we might need to create a new reader for the next chunk
	if err == io.EOF && n > 0 && vf.position < vf.virtualFile.Size {
		// We've reached the end of this chunk but there's more file to read
		// Close current reader so next Read() call will create a new reader for next chunk
		_ = vf.reader.Close()
		vf.reader = nil
		err = nil
	}

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

	// Early return for zero-length reads to prevent unnecessary reader creation
	if maxLen <= 0 {
		return 0, nil
	}

	end := off + maxLen - 1 // inclusive

	// Get HTTP range constraints to optimize reader creation
	rangeStart, rangeEnd, hasRange := vf.getRequestRange()
	if hasRange {
		// Validate that the requested read is within the HTTP range
		if off < rangeStart || off > rangeEnd {
			return 0, fmt.Errorf("read offset %d is outside requested range %d-%d", off, rangeStart, rangeEnd)
		}
		// Constrain end to not exceed the HTTP range
		if end > rangeEnd {
			end = rangeEnd
			maxLen = end - off + 1
		}
	}

	// Create reader with optimized range
	var reader io.ReadCloser
	var err error

	if vf.virtualFile.Encryption != nil && *vf.virtualFile.Encryption == "rclone" {
		reader, err = vf.wrapWithEncryption(off, end)
		if err != nil {
			return 0, fmt.Errorf("failed to wrap reader with encryption: %w", err)
		}
	} else {
		loader := dbSegmentLoader{segs: vf.nzbFile.SegmentsData}
		hasFixedSize := vf.nzbFile.SegmentSize > 0
		segSize := vf.nzbFile.SegmentSize
		rg := usenet.GetSegmentsInRange(off, end, loader, hasFixedSize, segSize)
		reader, err = usenet.NewUsenetReader(vf.ctx, vf.cp, rg, vf.maxWorkers)
		if err != nil {
			return 0, fmt.Errorf("failed to create usenet reader: %w", err)
		}
	}

	// Ensure reader is closed even if we panic or return early
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			// Log error but don't override return values
		}
	}()

	buf := p[:maxLen]
	n := 0
	for n < len(buf) {
		nn, rerr := reader.Read(buf[n:])
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

	// If we're seeking to a position far from current reader range, close the reader
	// This prevents memory leaks from keeping large readers open for distant positions
	if vf.reader != nil {
		// Calculate if the new position is outside a reasonable range from current position
		// Use 1MB threshold - if seeking more than 1MB away, close reader
		distance := abs - vf.position
		if distance < 0 {
			distance = -distance
		}

		if distance > 1024*1024 { // 1MB threshold
			_ = vf.reader.Close()
			vf.reader = nil
		}
	}

	vf.position = abs
	return abs, nil
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
