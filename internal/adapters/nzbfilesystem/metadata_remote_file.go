package nzbfilesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/encryption/rclone"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/altmount/internal/utils"
	"github.com/spf13/afero"
)

// MetadataRemoteFile implements the RemoteFile interface for metadata-backed virtual files
type MetadataRemoteFile struct {
	metadataService  *metadata.MetadataService
	healthRepository *database.HealthRepository
	poolManager      pool.Manager        // Pool manager for dynamic pool access
	configGetter     config.ConfigGetter // Dynamic config access
	rcloneCipher     encryption.Cipher   // For rclone encryption/decryption
}

// Configuration is now accessed dynamically through config.ConfigGetter
// No longer need a separate config struct

// NewMetadataRemoteFile creates a new metadata-based remote file handler
func NewMetadataRemoteFile(
	metadataService *metadata.MetadataService,
	healthRepository *database.HealthRepository,
	poolManager pool.Manager,
	configGetter config.ConfigGetter,
) *MetadataRemoteFile {
	// Initialize rclone cipher with global credentials for encrypted files
	cfg := configGetter()
	rcloneConfig := &encryption.Config{
		RclonePassword: cfg.RClone.Password, // Global password fallback
		RcloneSalt:     cfg.RClone.Salt,     // Global salt fallback
	}

	rcloneCipher, _ := rclone.NewRcloneCipher(rcloneConfig)

	return &MetadataRemoteFile{
		metadataService:  metadataService,
		healthRepository: healthRepository,
		poolManager:      poolManager,
		configGetter:     configGetter,
		rcloneCipher:     rcloneCipher,
	}
}

// Helper methods to get dynamic config values
func (mrf *MetadataRemoteFile) getMaxDownloadWorkers() int {
	return mrf.configGetter().Streaming.MaxDownloadWorkers
}

func (mrf *MetadataRemoteFile) getGlobalPassword() string {
	return mrf.configGetter().RClone.Password
}

func (mrf *MetadataRemoteFile) getGlobalSalt() string {
	return mrf.configGetter().RClone.Salt
}

func (mrf *MetadataRemoteFile) getMaxRangeSize() int64 {
	size := mrf.configGetter().Streaming.MaxRangeSize
	if size <= 0 {
		return 33554432 // Default 32MB
	}
	return size
}

func (mrf *MetadataRemoteFile) getStreamingChunkSize() int64 {
	size := mrf.configGetter().Streaming.StreamingChunkSize
	if size <= 0 {
		return 8388608 // Default 8MB
	}
	return size
}

// OpenFile opens a virtual file backed by metadata
func (mrf *MetadataRemoteFile) OpenFile(ctx context.Context, name string, r utils.PathWithArgs) (bool, afero.File, error) {
	// Forbid COPY operations - nzbfilesystem is read-only
	if r.IsCopy() {
		return false, nil, os.ErrPermission
	}

	// Normalize the path to handle trailing slashes consistently
	normalizedName := normalizePath(name)

	// Check if this is a directory first
	if mrf.metadataService.DirectoryExists(normalizedName) {
		// Create a directory handle
		virtualDir := &MetadataVirtualDirectory{
			name:            name,
			normalizedPath:  normalizedName,
			metadataService: mrf.metadataService,
		}
		return true, virtualDir, nil
	}

	// Check if this path exists as a file in our metadata
	exists := mrf.metadataService.FileExists(normalizedName)
	if !exists {
		// Check if this could be a valid empty directory
		if mrf.isValidEmptyDirectory(normalizedName) {
			// Create a directory handle for empty directory
			virtualDir := &MetadataVirtualDirectory{
				name:            name,
				normalizedPath:  normalizedName,
				metadataService: mrf.metadataService,
			}
			return true, virtualDir, nil
		}
		// Neither file nor directory found
		return false, nil, nil
	}

	// Get file metadata using simplified schema
	fileMeta, err := mrf.metadataService.ReadFileMetadata(normalizedName)
	if err != nil {
		return false, nil, fmt.Errorf("failed to read file metadata: %w", err)
	}

	if fileMeta == nil {
		return false, nil, nil
	}

	if fileMeta.Status == metapb.FileStatus_FILE_STATUS_CORRUPTED {
		return false, nil, ErrFileIsCorrupted
	}

	// Create a metadata-based virtual file handle
	virtualFile := &MetadataVirtualFile{
		name:               name,
		fileMeta:           fileMeta,
		metadataService:    mrf.metadataService,
		healthRepository:   mrf.healthRepository,
		args:               r,
		poolManager:        mrf.poolManager,
		ctx:                ctx,
		maxWorkers:         mrf.getMaxDownloadWorkers(),
		rcloneCipher:       mrf.rcloneCipher,
		globalPassword:     mrf.getGlobalPassword(),
		globalSalt:         mrf.getGlobalSalt(),
		maxRangeSize:       mrf.getMaxRangeSize(),
		streamingChunkSize: mrf.getStreamingChunkSize(),
	}

	return true, virtualFile, nil
}

// RemoveFile removes a virtual file from the metadata
func (mrf *MetadataRemoteFile) RemoveFile(ctx context.Context, fileName string) (bool, error) {
	// Normalize the path to handle trailing slashes consistently
	normalizedName := normalizePath(fileName)

	// Prevent removal of root directory
	if normalizedName == RootPath {
		return false, ErrCannotRemoveRoot
	}

	// Check if this path exists in our metadata
	exists := mrf.metadataService.FileExists(normalizedName)
	if !exists {
		// File not found in metadata
		return false, nil
	}

	// Use MetadataService's delete operation
	return true, mrf.metadataService.DeleteFileMetadata(normalizedName)
}

// RenameFile renames a virtual file in the metadata
func (mrf *MetadataRemoteFile) RenameFile(ctx context.Context, oldName, newName string) (bool, error) {
	// Normalize paths
	normalizedOld := normalizePath(oldName)
	normalizedNew := normalizePath(newName)

	// Check if old path exists
	exists := mrf.metadataService.FileExists(normalizedOld)
	if !exists {
		return false, nil
	}

	// Read existing metadata
	fileMeta, err := mrf.metadataService.ReadFileMetadata(normalizedOld)
	if err != nil {
		return false, fmt.Errorf("failed to read old metadata: %w", err)
	}

	// Write to new location
	if err := mrf.metadataService.WriteFileMetadata(normalizedNew, fileMeta); err != nil {
		return false, fmt.Errorf("failed to write new metadata: %w", err)
	}

	// Delete old location
	if err := mrf.metadataService.DeleteFileMetadata(normalizedOld); err != nil {
		return false, fmt.Errorf("failed to delete old metadata: %w", err)
	}

	return true, nil
}

// Stat returns file information for a path using metadata
func (mrf *MetadataRemoteFile) Stat(name string) (bool, fs.FileInfo, error) {
	// Normalize the path
	normalizedName := normalizePath(name)

	// Check if this is a directory first
	if mrf.metadataService.DirectoryExists(normalizedName) {
		info := &MetadataFileInfo{
			name:    filepath.Base(normalizedName),
			size:    0,
			mode:    os.ModeDir | 0755,
			modTime: time.Now(), // Use current time for directories
			isDir:   true,
		}
		return true, info, nil
	}

	// Check if this path exists as a file in our metadata
	exists := mrf.metadataService.FileExists(normalizedName)
	if !exists {
		return false, nil, fs.ErrNotExist
	}

	// Get file metadata using simplified schema
	fileMeta, err := mrf.metadataService.ReadFileMetadata(normalizedName)
	if err != nil {
		return false, nil, fmt.Errorf("failed to read file metadata: %w", err)
	}

	if fileMeta == nil {
		return false, nil, fs.ErrNotExist
	}

	// Convert to fs.FileInfo
	info := &MetadataFileInfo{
		name:    filepath.Base(normalizedName),
		size:    fileMeta.FileSize,
		mode:    0644, // Default file mode
		modTime: time.Unix(fileMeta.ModifiedAt, 0),
		isDir:   false,
	}

	return true, info, nil
}

// MetadataFileInfo implements fs.FileInfo for metadata-based files
type MetadataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (mfi *MetadataFileInfo) Name() string       { return mfi.name }
func (mfi *MetadataFileInfo) Size() int64        { return mfi.size }
func (mfi *MetadataFileInfo) Mode() os.FileMode  { return mfi.mode }
func (mfi *MetadataFileInfo) ModTime() time.Time { return mfi.modTime }
func (mfi *MetadataFileInfo) IsDir() bool        { return mfi.isDir }
func (mfi *MetadataFileInfo) Sys() interface{}   { return nil }

// MetadataSegmentLoader adapts metadata segments to the usenet.SegmentLoader interface
type MetadataSegmentLoader struct {
	segments []*metapb.SegmentData
}

// newMetadataSegmentLoader creates a new metadata segment loader
func newMetadataSegmentLoader(segments []*metapb.SegmentData) *MetadataSegmentLoader {
	return &MetadataSegmentLoader{
		segments: segments,
	}
}

// GetSegment implements usenet.SegmentLoader interface
func (msl *MetadataSegmentLoader) GetSegment(index int) (segment usenet.Segment, groups []string, ok bool) {
	if index < 0 || index >= len(msl.segments) {
		return usenet.Segment{}, nil, false
	}

	seg := msl.segments[index]

	// Important: range builder only knows (Start, Size) and assumes usable bytes are [Start, Size-1].
	// Our metadata may have a trimmed EndOffset (< SegmentSize-1). Provide a synthetic Size = EndOffset+1
	// so usable length becomes (EndOffset - StartOffset + 1) and no extra tail bytes are exposed.
	size := seg.SegmentSize
	if seg.EndOffset > 0 && seg.EndOffset < seg.SegmentSize-1 {
		size = seg.EndOffset + 1
	}

	// Keep original start offset (could be >0 due to RAR processing) and size
	return usenet.Segment{
		Id:    seg.Id,
		Start: seg.StartOffset,
		Size:  size,
	}, []string{}, true // Empty groups for now - could be stored in metadata if needed
}

// MetadataVirtualDirectory implements afero.File for metadata-backed virtual directories
type MetadataVirtualDirectory struct {
	name            string
	normalizedPath  string
	metadataService *metadata.MetadataService
}

// Read implements afero.File.Read (not supported for directories)
func (mvd *MetadataVirtualDirectory) Read(p []byte) (n int, err error) {
	return 0, ErrCannotReadDirectory
}

// ReadAt implements afero.File.ReadAt (not supported for directories)
func (mvd *MetadataVirtualDirectory) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, ErrCannotReadDirectory
}

// Seek implements afero.File.Seek (not supported for directories)
func (mvd *MetadataVirtualDirectory) Seek(offset int64, whence int) (int64, error) {
	return 0, ErrCannotReadDirectory
}

// Close implements afero.File.Close
func (mvd *MetadataVirtualDirectory) Close() error {
	return nil
}

// Name implements afero.File.Name
func (mvd *MetadataVirtualDirectory) Name() string {
	return mvd.name
}

// Readdir implements afero.File.Readdir
func (mvd *MetadataVirtualDirectory) Readdir(count int) ([]fs.FileInfo, error) {
	// Create metadata reader for directory operations
	reader := metadata.NewMetadataReader(mvd.metadataService)

	// Get directory contents - we only need the directory infos, not the file metadata
	dirInfos, _, err := reader.ListDirectoryContents(mvd.normalizedPath)
	if err != nil {
		return nil, err
	}

	var infos []fs.FileInfo

	// Add directories first
	for _, dirInfo := range dirInfos {
		// Skip the current directory itself
		if dirInfo.Name() == filepath.Base(mvd.normalizedPath) || dirInfo.Name() == "." {
			continue
		}
		infos = append(infos, dirInfo)
		if count > 0 && len(infos) >= count {
			return infos, nil
		}
	}

	// Add files - we need to get the virtual filename from the metadata path
	// Since ListDirectoryContents already reads the metadata files, we need to get the filenames differently
	// Let's use the metadata service directly to list files in the directory
	fileNames, err := mvd.metadataService.ListDirectory(mvd.normalizedPath)
	if err != nil {
		return nil, err
	}

	for _, fileName := range fileNames {
		virtualFilePath := filepath.Join(mvd.normalizedPath, fileName)
		fileMeta, err := mvd.metadataService.ReadFileMetadata(virtualFilePath)
		if err != nil || fileMeta == nil {
			continue
		}

		info := &MetadataFileInfo{
			name:    fileName, // Use the actual virtual filename from the metadata filesystem
			size:    fileMeta.FileSize,
			mode:    0644,
			modTime: time.Unix(fileMeta.ModifiedAt, 0),
			isDir:   false,
		}
		infos = append(infos, info)
		if count > 0 && len(infos) >= count {
			return infos, nil
		}
	}

	return infos, nil
}

// Readdirnames implements afero.File.Readdirnames
func (mvd *MetadataVirtualDirectory) Readdirnames(n int) ([]string, error) {
	infos, err := mvd.Readdir(n)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.Name()
	}

	return names, nil
}

// Stat implements afero.File.Stat
func (mvd *MetadataVirtualDirectory) Stat() (fs.FileInfo, error) {
	info := &MetadataFileInfo{
		name:    filepath.Base(mvd.normalizedPath),
		size:    0,
		mode:    os.ModeDir | 0755,
		modTime: time.Now(),
		isDir:   true,
	}
	return info, nil
}

// Write implements afero.File.Write (not supported)
func (mvd *MetadataVirtualDirectory) Write(p []byte) (n int, err error) {
	return 0, os.ErrPermission
}

// WriteAt implements afero.File.WriteAt (not supported)
func (mvd *MetadataVirtualDirectory) WriteAt(p []byte, off int64) (n int, err error) {
	return 0, os.ErrPermission
}

// WriteString implements afero.File.WriteString (not supported)
func (mvd *MetadataVirtualDirectory) WriteString(s string) (ret int, err error) {
	return 0, os.ErrPermission
}

// Sync implements afero.File.Sync (no-op for directories)
func (mvd *MetadataVirtualDirectory) Sync() error {
	return nil
}

// Truncate implements afero.File.Truncate (not supported)
func (mvd *MetadataVirtualDirectory) Truncate(size int64) error {
	return os.ErrPermission
}

// MetadataVirtualFile implements afero.File for metadata-backed virtual files
type MetadataVirtualFile struct {
	name             string
	fileMeta         *metapb.FileMetadata
	metadataService  *metadata.MetadataService
	healthRepository *database.HealthRepository
	args             utils.PathWithArgs
	poolManager      pool.Manager // Pool manager for dynamic pool access
	ctx              context.Context
	maxWorkers       int
	rcloneCipher     encryption.Cipher
	globalPassword   string
	globalSalt       string

	// Reader state and position tracking
	reader            io.ReadCloser
	readerInitialized bool
	position          int64
	currentRangeStart int64 // Start of current reader's range
	currentRangeEnd   int64 // End of current reader's range
	originalRangeEnd  int64 // Original end requested by client (-1 for unbounded)

	// Configurable range settings
	maxRangeSize       int64 // Maximum range size for a single request
	streamingChunkSize int64 // Chunk size for streaming when end=-1

	mu sync.Mutex
}

// Read implements afero.File.Read
func (mvf *MetadataVirtualFile) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	mvf.mu.Lock()
	defer mvf.mu.Unlock()

	if err := mvf.ensureReader(); err != nil {
		return 0, err
	}

	totalRead, err := mvf.reader.Read(p)
	if err != nil {
		// Check if this is EOF and we have more data to read
		if errors.Is(err, io.EOF) && mvf.hasMoreDataToRead() {
			// Close current reader and create a new one for the next range
			mvf.closeCurrentReader()

			// Try to create a new reader for the remaining data
			if newReaderErr := mvf.ensureReader(); newReaderErr != nil {
				// If we can't create a new reader, return what we have
				mvf.position += int64(totalRead)
				return totalRead, err
			}

			// Try to read more data with the new reader
			if totalRead < len(p) {
				additionalRead, newErr := mvf.reader.Read(p[totalRead:])
				totalRead += additionalRead
				if newErr != nil && !errors.Is(newErr, io.EOF) {
					err = newErr
				} else if newErr == nil {
					err = nil // Clear EOF if we successfully read more
				}
			}
		}

		var articleErr *usenet.ArticleNotFoundError
		// Handle UsenetReader errors the same way as VirtualFile
		if errors.As(err, &articleErr) {
			// Update file health status and database tracking
			mvf.updateFileHealthOnError(articleErr, articleErr.BytesRead > 0 || totalRead > 0)

			if articleErr.BytesRead > 0 || totalRead > 0 {
				// Some content was read - return partial content error
				return totalRead, &PartialContentError{
					BytesRead:     articleErr.BytesRead,
					TotalExpected: mvf.fileMeta.FileSize,
					UnderlyingErr: articleErr,
				}
			} else {
				// No content read - return corrupted file error
				return totalRead, &CorruptedFileError{
					TotalExpected: mvf.fileMeta.FileSize,
					UnderlyingErr: articleErr,
				}
			}
		}

		// Update position even on error if we read some data
		mvf.position += int64(totalRead)
		return totalRead, err
	}

	// Update the current position after reading
	mvf.position += int64(totalRead)
	return totalRead, nil
}

// ReadAt implements afero.File.ReadAt
func (mvf *MetadataVirtualFile) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, os.ErrPermission // Not supported for streaming readers
}

// Seek implements afero.File.Seek
func (mvf *MetadataVirtualFile) Seek(offset int64, whence int) (int64, error) {
	mvf.mu.Lock()
	defer mvf.mu.Unlock()

	var abs int64

	switch whence {
	case io.SeekStart: // Relative to the origin of the file
		abs = offset
	case io.SeekCurrent: // Relative to the current offset
		abs = mvf.position + offset
	case io.SeekEnd: // Relative to the end
		abs = mvf.fileMeta.FileSize + offset
	default:
		return 0, ErrInvalidWhence
	}

	if abs < 0 {
		return 0, ErrSeekNegative
	}

	if abs > mvf.fileMeta.FileSize {
		return 0, ErrSeekTooFar
	}

	// Check if the new position is outside our current reader's range
	if mvf.readerInitialized && (abs < mvf.currentRangeStart || abs > mvf.currentRangeEnd) {
		// Position is outside current range, need to recreate reader
		mvf.closeCurrentReader()
	}

	// Update position - new reader will be created on next read if needed
	mvf.position = abs
	return abs, nil
}

// Close implements afero.File.Close
func (mvf *MetadataVirtualFile) Close() error {
	mvf.mu.Lock()
	defer mvf.mu.Unlock()
	if mvf.reader != nil {
		err := mvf.reader.Close()
		mvf.reader = nil
		mvf.readerInitialized = false
		return err
	}
	return nil
}

// Name implements afero.File.Name
func (mvf *MetadataVirtualFile) Name() string {
	return mvf.name
}

// Readdir implements afero.File.Readdir
func (mvf *MetadataVirtualFile) Readdir(count int) ([]fs.FileInfo, error) {
	// This is a file, not a directory, so readdir is not supported
	return nil, ErrNotDirectory
}

// Readdirnames implements afero.File.Readdirnames
func (mvf *MetadataVirtualFile) Readdirnames(n int) ([]string, error) {
	infos, err := mvf.Readdir(n)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.Name()
	}

	return names, nil
}

// Stat implements afero.File.Stat
func (mvf *MetadataVirtualFile) Stat() (fs.FileInfo, error) {
	info := &MetadataFileInfo{
		name:    filepath.Base(mvf.name),
		size:    mvf.fileMeta.FileSize,
		mode:    0644,
		modTime: time.Unix(mvf.fileMeta.ModifiedAt, 0),
		isDir:   false, // Files are never directories in simplified schema
	}

	return info, nil
}

// Write implements afero.File.Write (not supported)
func (mvf *MetadataVirtualFile) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("write not supported")
}

// WriteAt implements afero.File.WriteAt (not supported)
func (mvf *MetadataVirtualFile) WriteAt(p []byte, off int64) (n int, err error) {
	return 0, fmt.Errorf("write not supported")
}

// WriteString implements afero.File.WriteString (not supported)
func (mvf *MetadataVirtualFile) WriteString(s string) (ret int, err error) {
	return 0, fmt.Errorf("write not supported")
}

// Sync implements afero.File.Sync (no-op for read-only)
func (mvf *MetadataVirtualFile) Sync() error {
	return nil
}

// Truncate implements afero.File.Truncate (not supported)
func (mvf *MetadataVirtualFile) Truncate(size int64) error {
	return fmt.Errorf("truncate not supported")
}

// hasMoreDataToRead checks if there's more data to read beyond current range
func (mvf *MetadataVirtualFile) hasMoreDataToRead() bool {
	// If we have an original range end and haven't reached it, there's more to read
	if mvf.originalRangeEnd != -1 && mvf.position < mvf.originalRangeEnd {
		return true
	}
	// If original range was unbounded (-1) and we haven't reached file end, there's more to read
	if mvf.originalRangeEnd == -1 && mvf.position < mvf.fileMeta.FileSize {
		return true
	}
	return false
}

// closeCurrentReader closes the current reader and marks it uninitialized
func (mvf *MetadataVirtualFile) closeCurrentReader() {
	if mvf.reader != nil {
		mvf.reader.Close()
		mvf.reader = nil
	}
	mvf.readerInitialized = false
}

// ensureReader ensures we have a reader initialized for the current position with range support
func (mvf *MetadataVirtualFile) ensureReader() error {
	if mvf.readerInitialized {
		return nil
	}

	if mvf.poolManager == nil {
		return ErrNoUsenetPool
	}

	// Get request range from args or use default range starting from current position
	start, end := mvf.getRequestRange()

	// Track the current reader's range for progressive reading
	mvf.currentRangeStart = start
	mvf.currentRangeEnd = end

	// Create reader for the calculated range using metadata segments
	if mvf.fileMeta.Encryption != metapb.Encryption_NONE {
		// Wrap the usenet reader with encryption
		decryptedReader, err := mvf.wrapWithEncryption(start, end)
		if err != nil {
			return fmt.Errorf(ErrMsgFailedWrapEncryption, err)
		}
		mvf.reader = decryptedReader
	} else {
		// Create plain usenet reader
		ur, err := mvf.createUsenetReader(mvf.ctx, start, end)
		if err != nil {
			return err
		}
		mvf.reader = ur
	}

	mvf.readerInitialized = true
	return nil
}

// getRequestRange gets the range for reader creation based on HTTP range or current position
// Implements intelligent range limiting to prevent excessive memory usage when end=-1 or ranges are too large
func (mvf *MetadataVirtualFile) getRequestRange() (start, end int64) {
	// If this is the first read, check for HTTP range header and save original end
	if !mvf.readerInitialized && mvf.originalRangeEnd == 0 {
		rangeHeader, err := mvf.args.Range()
		if err == nil && rangeHeader != nil {
			mvf.originalRangeEnd = rangeHeader.End
			return mvf.calculateIntelligentRange(rangeHeader.Start, rangeHeader.End)
		} else {
			// No range header, set unbounded
			mvf.originalRangeEnd = -1
			return mvf.calculateIntelligentRange(mvf.position, -1)
		}
	}

	// For subsequent reads, use current position and respect original range
	var targetEnd int64
	if mvf.originalRangeEnd == -1 {
		// Original was unbounded, continue unbounded
		targetEnd = -1
	} else {
		// Original had an end, respect it
		targetEnd = mvf.originalRangeEnd
	}

	return mvf.calculateIntelligentRange(mvf.position, targetEnd)
}

// calculateIntelligentRange applies intelligent range limiting to prevent excessive memory usage
// Only applies limiting when end=-1 or when the requested range exceeds safe memory limits
func (mvf *MetadataVirtualFile) calculateIntelligentRange(start, end int64) (int64, int64) {
	fileSize := mvf.fileMeta.FileSize

	// Handle empty files - return invalid range that will result in no segments
	if fileSize == 0 {
		return 0, -1 // Invalid range for empty file
	}

	// Ensure start is within bounds
	if start < 0 {
		start = 0
	}
	if start >= fileSize {
		start = fileSize - 1
	}

	// Handle end = -1 (to end of file) with intelligent limiting
	if end == -1 {
		// Calculate a reasonable chunk size based on remaining file size
		remaining := fileSize - start

		// If remaining size is smaller than configured streaming chunk size, use all remaining
		if remaining <= mvf.streamingChunkSize {
			end = fileSize - 1
		} else {
			// Limit to configured streaming chunk size to prevent excessive memory usage
			end = start + mvf.streamingChunkSize - 1
		}
	} else {
		// Ensure end is within file bounds
		if end >= fileSize {
			end = fileSize - 1
		}

		// Only apply maximum range size limit if the requested range is excessively large
		// This preserves the original range request for reasonable sizes
		rangeSize := end - start + 1
		if rangeSize > mvf.maxRangeSize {
			end = start + mvf.maxRangeSize - 1
			// Ensure we don't exceed file size
			if end >= fileSize {
				end = fileSize - 1
			}
		}
		// If rangeSize <= maxRangeSize, preserve the original range as-is
	}

	return start, end
}

// createUsenetReader creates a new usenet reader for the specified range using metadata segments
func (mvf *MetadataVirtualFile) createUsenetReader(ctx context.Context, start, end int64) (io.ReadCloser, error) {
	if len(mvf.fileMeta.SegmentData) == 0 {
		return nil, ErrNoNzbData
	}

	// Get connection pool dynamically from pool manager
	cp, err := mvf.poolManager.GetPool()
	if err != nil {
		return nil, fmt.Errorf("failed to get connection pool: %w", err)
	}

	loader := newMetadataSegmentLoader(mvf.fileMeta.SegmentData)
	rg := usenet.GetSegmentsInRange(start, end, loader)
	return usenet.NewUsenetReader(ctx, cp, rg, mvf.maxWorkers)
}

// wrapWithEncryption wraps a usenet reader with encryption using metadata
func (mvf *MetadataVirtualFile) wrapWithEncryption(start, end int64) (io.ReadCloser, error) {
	if mvf.fileMeta.Encryption == metapb.Encryption_NONE {
		return nil, ErrNoEncryptionParams
	}

	var cipher encryption.Cipher
	switch mvf.fileMeta.Encryption {
	case metapb.Encryption_RCLONE:
		if mvf.rcloneCipher == nil {
			return nil, ErrNoCipherConfig
		}
		cipher = mvf.rcloneCipher
	default:
		return nil, fmt.Errorf("unsupported encryption type: %v", mvf.fileMeta.Encryption)
	}

	// Get password and salt from metadata, with global fallback
	password := mvf.fileMeta.Password
	if password == "" {
		password = mvf.globalPassword
	}

	salt := mvf.fileMeta.Salt
	if salt == "" {
		salt = mvf.globalSalt
	}

	decryptedReader, err := cipher.Open(
		mvf.ctx,
		&utils.RangeHeader{
			Start: start,
			End:   end,
		},
		mvf.fileMeta.FileSize,
		password,
		salt,
		func(ctx context.Context, start, end int64) (io.ReadCloser, error) {
			return mvf.createUsenetReader(ctx, start, end)
		},
	)
	if err != nil {
		return nil, fmt.Errorf(ErrMsgFailedCreateDecryptReader, err)
	}

	return decryptedReader, nil
}

// updateFileHealthOnError updates both metadata and database health status when corruption is detected
func (mvf *MetadataVirtualFile) updateFileHealthOnError(articleErr *usenet.ArticleNotFoundError, hasPartialContent bool) {
	// Determine the appropriate status
	var metadataStatus metapb.FileStatus
	var dbStatus database.HealthStatus

	if hasPartialContent {
		metadataStatus = metapb.FileStatus_FILE_STATUS_PARTIAL
		dbStatus = database.HealthStatusPartial
	} else {
		metadataStatus = metapb.FileStatus_FILE_STATUS_CORRUPTED
		dbStatus = database.HealthStatusCorrupted
	}

	// Update metadata status (non-blocking)
	go func() {
		if err := mvf.metadataService.UpdateFileStatus(mvf.name, metadataStatus); err != nil {
			// Log error but don't fail the read operation
			fmt.Printf("Warning: failed to update metadata status for %s: %v\n", mvf.name, err)
		}
	}()

	// Update database health tracking (non-blocking)
	go func() {
		errorMsg := articleErr.Error()
		sourceNzbPath := &mvf.fileMeta.SourceNzbPath
		if *sourceNzbPath == "" {
			sourceNzbPath = nil
		}

		// Create error details JSON
		errorDetails := fmt.Sprintf(`{"missing_articles": %d, "total_articles": %d, "error_type": "ArticleNotFound"}`,
			1, len(mvf.fileMeta.SegmentData)) // Simplified, could be enhanced

		if err := mvf.healthRepository.UpdateFileHealth(
			mvf.name,
			dbStatus,
			&errorMsg,
			sourceNzbPath,
			&errorDetails,
		); err != nil {
			fmt.Printf("Warning: failed to update file health for %s: %v\n", mvf.name, err)
		}
	}()
}

// isValidEmptyDirectory checks if a path could represent a valid empty directory
// by examining parent directories and path structure
func (mrf *MetadataRemoteFile) isValidEmptyDirectory(normalizedPath string) bool {
	// Root directory is always valid
	if normalizedPath == RootPath {
		return true
	}

	// Get parent directory
	parentDir := filepath.Dir(normalizedPath)
	if parentDir == "." {
		parentDir = RootPath
	}

	// Check if parent directory exists (either physically or as a valid empty directory)
	if mrf.metadataService.DirectoryExists(parentDir) {
		return true
	}

	// Recursively check if parent could be a valid empty directory
	return mrf.isValidEmptyDirectory(parentDir)
}

// Dynamic configuration is now handled through config getters
// No longer need update methods - values are accessed directly from current config
