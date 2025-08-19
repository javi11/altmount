package nzbfilesystem

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/encryption/headers"
	"github.com/javi11/altmount/internal/encryption/rclone"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/altmount/internal/utils"
	"github.com/javi11/nntppool"
	"github.com/spf13/afero"
)

// MetadataRemoteFile implements the RemoteFile interface for metadata-backed virtual files
type MetadataRemoteFile struct {
	metadataService    *metadata.MetadataService
	cp                 nntppool.UsenetConnectionPool
	maxDownloadWorkers int
	rcloneCipher       encryption.Cipher // For rclone encryption/decryption
	headersCipher      encryption.Cipher // For headers encryption/decryption
	globalPassword     string            // Global password fallback
	globalSalt         string            // Global salt fallback
}

// MetadataRemoteFileConfig holds configuration for MetadataRemoteFile
type MetadataRemoteFileConfig struct {
	GlobalPassword string
	GlobalSalt     string
}

// NewMetadataRemoteFile creates a new metadata-based remote file handler
func NewMetadataRemoteFile(
	metadataService *metadata.MetadataService,
	cp nntppool.UsenetConnectionPool,
	maxDownloadWorkers int,
	config MetadataRemoteFileConfig,
) *MetadataRemoteFile {
	// Initialize rclone cipher with global credentials for encrypted files
	rcloneConfig := &encryption.Config{
		RclonePassword: config.GlobalPassword, // Global password fallback
		RcloneSalt:     config.GlobalSalt,     // Global salt fallback
	}

	rcloneCipher, _ := rclone.NewRcloneCipher(rcloneConfig)
	headersCipher, _ := headers.NewHeadersCipher()

	return &MetadataRemoteFile{
		metadataService:    metadataService,
		cp:                 cp,
		maxDownloadWorkers: maxDownloadWorkers,
		rcloneCipher:       rcloneCipher,
		headersCipher:      headersCipher,
		globalPassword:     config.GlobalPassword,
		globalSalt:         config.GlobalSalt,
	}
}

// OpenFile opens a virtual file backed by metadata
func (mrf *MetadataRemoteFile) OpenFile(ctx context.Context, name string, r utils.PathWithArgs) (bool, afero.File, error) {
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

	// Create a metadata-based virtual file handle
	virtualFile := &MetadataVirtualFile{
		name:            name,
		fileMeta:        fileMeta,
		metadataService: mrf.metadataService,
		args:            r,
		cp:              mrf.cp,
		ctx:             ctx,
		maxWorkers:      mrf.maxDownloadWorkers,
		rcloneCipher:    mrf.rcloneCipher,
		headersCipher:   mrf.headersCipher,
		globalPassword:  mrf.globalPassword,
		globalSalt:      mrf.globalSalt,
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
	return usenet.Segment{
		Id:    seg.Id,
		Start: seg.StartOffset,
		End:   seg.EndOffset,
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
	name            string
	fileMeta        *metapb.FileMetadata
	metadataService *metadata.MetadataService
	args            utils.PathWithArgs
	cp              nntppool.UsenetConnectionPool
	ctx             context.Context
	maxWorkers      int
	rcloneCipher    encryption.Cipher
	headersCipher   encryption.Cipher
	globalPassword  string
	globalSalt      string

	// Reader state and position tracking
	reader            io.ReadCloser
	readerInitialized bool
	position          int64
	mu                sync.Mutex
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
		// Handle UsenetReader errors the same way as VirtualFile
		if articleErr, ok := err.(*usenet.ArticleNotFoundError); ok {
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

	// Update position - reader will handle the actual seeking
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

// ensureReader ensures we have a reader initialized for the current position with range support
func (mvf *MetadataVirtualFile) ensureReader() error {
	if mvf.readerInitialized {
		return nil
	}

	if mvf.cp == nil {
		return ErrNoUsenetPool
	}

	// Get request range from args or use default range starting from current position
	start, end, _ := mvf.getRequestRange()

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
func (mvf *MetadataVirtualFile) getRequestRange() (start, end int64, hasRange bool) {
	// Try to get range from HTTP request args
	rangeHeader, err := mvf.args.Range()
	if err != nil || rangeHeader == nil {
		// No valid range header, return range from current position to end
		return mvf.position, mvf.fileMeta.FileSize - 1, false
	}

	// Fix range header to ensure it's within file bounds
	fixedRange := utils.FixRangeHeader(rangeHeader, mvf.fileMeta.FileSize)
	if fixedRange == nil {
		return mvf.position, mvf.fileMeta.FileSize - 1, false
	}

	// Ensure range is valid
	if fixedRange.Start < 0 {
		fixedRange.Start = 0
	}
	if fixedRange.End >= mvf.fileMeta.FileSize {
		fixedRange.End = mvf.fileMeta.FileSize - 1
	}
	if fixedRange.Start > fixedRange.End {
		return mvf.position, mvf.fileMeta.FileSize - 1, false
	}

	return fixedRange.Start, fixedRange.End, true
}

// createUsenetReader creates a new usenet reader for the specified range using metadata segments
func (mvf *MetadataVirtualFile) createUsenetReader(ctx context.Context, start, end int64) (io.ReadCloser, error) {
	if len(mvf.fileMeta.SegmentData) == 0 {
		return nil, ErrNoNzbData
	}

	loader := newMetadataSegmentLoader(mvf.fileMeta.SegmentData)
	rg := usenet.GetSegmentsInRange(start, end, loader)
	return usenet.NewUsenetReader(ctx, mvf.cp, rg, mvf.maxWorkers)
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
	case metapb.Encryption_HEADERS:
		if mvf.headersCipher == nil {
			return nil, ErrNoCipherConfig
		}
		cipher = mvf.headersCipher
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
