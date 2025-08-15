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
	"github.com/javi11/altmount/internal/nzb"
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

	// Check if this file is extracted from a RAR archive
	isRarFile, err := vf.isExtractedFromRar()
	if err != nil {
		return fmt.Errorf("failed to check if file is from RAR: %w", err)
	}

	if isRarFile {
		// For RAR files, create a RAR content reader instead of direct Usenet reader
		return vf.ensureRarReader()
	}

	start, end, _ := vf.getRequestRange()

	// Create reader for the calculated range
	if vf.virtualFile.Encryption != nil {
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
	if vf.virtualFile.VirtualPath == RootPath {
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

// isExtractedFromRar checks if this virtual file was extracted from a RAR archive
func (vf *VirtualFile) isExtractedFromRar() (bool, error) {
	if vf.db == nil || vf.virtualFile == nil {
		return false, nil
	}

	repo := database.NewRepository(vf.db.Connection())
	metadata, err := repo.GetFileMetadata(vf.virtualFile.ID)
	if err != nil {
		// If metadata doesn't exist, it's not an error - just means it's not from RAR
		return false, nil
	}

	_, exists := metadata["extracted_from_rar"]
	return exists, nil
}

// ensureRarReaderForPosition creates a reader for RAR content at the specified position
func (vf *VirtualFile) ensureRarReader() error {
	// Create RAR content reader with seek support
	rarReader, err := vf.createRarContentReader()
	if err != nil {
		return fmt.Errorf("failed to create RAR content reader: %w", err)
	}

	start, _, _ := vf.getRequestRange()

	// Seek to the start position in the RAR content
	position, err := rarReader.Seek(start, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek in RAR content: %w", err)
	}

	vf.reader = rarReader
	vf.position = position
	return nil
}

// readAtFromRar reads data from a specific offset in a RAR-extracted file
func (vf *VirtualFile) readAtFromRar(p []byte, off int64) (int, error) {
	// Create a new RAR content reader for this specific read operation
	rarReader, err := vf.createRarContentReader()
	if err != nil {
		return 0, fmt.Errorf("failed to create RAR content reader: %w", err)
	}
	defer rarReader.Close()

	// Seek to the desired offset using the seeker interface
	_, err = rarReader.Seek(off, io.SeekStart)
	if err != nil {
		return 0, fmt.Errorf("failed to seek in RAR content: %w", err)
	}

	// Read the requested data
	return io.ReadFull(rarReader, p)
}

// createRarContentReader creates a streaming reader with seek support for this RAR-extracted file
func (vf *VirtualFile) createRarContentReader() (nzb.RarContentReadSeeker, error) {
	if vf.db == nil || vf.virtualFile == nil || vf.nzbFile == nil {
		return nil, fmt.Errorf("missing database or file information")
	}

	// Get the RAR directory path from metadata
	repo := database.NewRepository(vf.db.Connection())
	metadata, err := repo.GetFileMetadata(vf.virtualFile.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata: %w", err)
	}

	rarDirPath, exists := metadata["extracted_from_rar"]
	if !exists || rarDirPath == "" {
		return nil, fmt.Errorf("file is not extracted from RAR or missing RAR directory metadata")
	}

	// Get the parent RAR directory to find the RAR content
	// First, find the RAR directory virtual file
	rarDirFile, err := repo.GetVirtualFileByPath(rarDirPath)
	if err != nil || rarDirFile == nil {
		return nil, fmt.Errorf("failed to find RAR directory: %s", rarDirPath)
	}

	// Get the RAR content information for this directory
	rarContents, err := repo.GetRarContentsByVirtualFileID(rarDirFile.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get RAR contents for directory ID %d: %w", rarDirFile.ID, err)
	}

	if len(rarContents) == 0 {
		return nil, fmt.Errorf("no RAR contents found for directory %s", rarDirPath)
	}

	// Find RAR files associated with this NZB
	rarFiles, err := vf.getRarFilesFromNzb()
	if err != nil {
		return nil, fmt.Errorf("failed to get RAR files from NZB: %w", err)
	}

	if len(rarFiles) == 0 {
		return nil, fmt.Errorf("no RAR files found in NZB")
	}

	// Find the RAR content entry for this specific file to get its offset and size
	var rarContent *database.RarContent
	for _, content := range rarContents {
		if content.Filename == vf.virtualFile.Filename || content.InternalPath == vf.virtualFile.Filename {
			rarContent = content
			break
		}
	}

	if rarContent == nil {
		return nil, fmt.Errorf("RAR content not found for file: %s", vf.virtualFile.Filename)
	}

	if rarContent.FileOffset == nil {
		return nil, fmt.Errorf("file offset not available for RAR file: %s", vf.virtualFile.Filename)
	}

	// Use the RAR handler to create a direct content reader for optimal performance
	// This bypasses rardecode for content reading and streams directly from Usenet using the file offset
	rarHandler := nzb.NewRarHandler(vf.cp, vf.maxWorkers)
	targetFilename := vf.virtualFile.Filename // The filename within the RAR archive

	return rarHandler.CreateDirectRarContentReader(
		vf.ctx,
		vf.nzbFile,
		rarFiles,
		targetFilename,
		*rarContent.FileOffset, // Starting offset of the file within the RAR stream
		rarContent.Size,        // Size of the target file
	)
}

// getRarFilesFromNzb extracts RAR file information from the database
func (vf *VirtualFile) getRarFilesFromNzb() ([]nzb.ParsedFile, error) {
	// NEW IMPLEMENTATION: Query the database for RAR part NZB files
	// With the enhanced database schema, each RAR part is stored as a separate NZB record
	// with its proper filename and corresponding segments

	if vf.db == nil || vf.nzbFile == nil {
		return nil, fmt.Errorf("missing database or NZB file information")
	}

	repo := database.NewRepository(vf.db.Connection())

	// Get all RAR part NZB files for this parent NZB
	rarPartNzbFiles, err := repo.GetRarPartNzbFiles(vf.nzbFile.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get RAR part NZB files: %w", err)
	}

	if len(rarPartNzbFiles) == 0 {
		return nil, fmt.Errorf("no RAR part files found for NZB ID %d", vf.nzbFile.ID)
	}

	// Convert NZB files to ParsedFiles for the RAR handler
	var rarFiles []nzb.ParsedFile
	for _, nzbFile := range rarPartNzbFiles {
		rarFile := nzb.ParsedFile{
			Filename:     nzbFile.Filename,     // Actual RAR part filename (movie.rar, movie.r00, etc.)
			Size:         nzbFile.Size,         // Size of this specific part
			Segments:     nzbFile.SegmentsData, // Only segments for this RAR part
			IsRarArchive: true,                 // All are RAR archive parts
		}
		rarFiles = append(rarFiles, rarFile)
	}

	return rarFiles, nil
}
