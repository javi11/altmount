package nzbfilesystem

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
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
	rcloneCipher := encryption.NewRCloneCipher(config.GlobalPassword, config.GlobalSalt)
	headersCipher := encryption.NewHeadersCipher(config.GlobalPassword, config.GlobalSalt)

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

	// Check if this path exists in our metadata
	exists := mrf.metadataService.FileExists(normalizedName)
	if !exists {
		// File not found in metadata
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
		name:               name,
		fileMeta:           fileMeta,
		metadataService:    mrf.metadataService,
		args:               r,
		cp:                 mrf.cp,
		ctx:                ctx,
		maxWorkers:         mrf.maxDownloadWorkers,
		rcloneCipher:       mrf.rcloneCipher,
		headersCipher:      mrf.headersCipher,
		globalPassword:     mrf.globalPassword,
		globalSalt:         mrf.globalSalt,
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

// MetadataVirtualFile implements afero.File for metadata-backed virtual files
type MetadataVirtualFile struct {
	name               string
	fileMeta           *metapb.FileMetadata
	metadataService    *metadata.MetadataService
	args               utils.PathWithArgs
	cp                 nntppool.UsenetConnectionPool
	ctx                context.Context
	maxWorkers         int
	rcloneCipher       encryption.Cipher
	headersCipher      encryption.Cipher
	globalPassword     string
	globalSalt         string
	
	// Lazy initialization
	reader             afero.File
	readerInitialized  bool
}

// Read implements afero.File.Read
func (mvf *MetadataVirtualFile) Read(p []byte) (n int, error) {
	if err := mvf.initReader(); err != nil {
		return 0, err
	}
	return mvf.reader.Read(p)
}

// ReadAt implements afero.File.ReadAt
func (mvf *MetadataVirtualFile) ReadAt(p []byte, off int64) (n int, error) {
	if err := mvf.initReader(); err != nil {
		return 0, err
	}
	// For now, simulate ReadAt using Seek + Read
	if seeker, ok := mvf.reader.(io.Seeker); ok {
		if _, err := seeker.Seek(off, io.SeekStart); err != nil {
			return 0, err
		}
		return mvf.reader.Read(p)
	}
	return 0, fmt.Errorf("ReadAt not supported")
}

// Seek implements afero.File.Seek
func (mvf *MetadataVirtualFile) Seek(offset int64, whence int) (int64, error) {
	if err := mvf.initReader(); err != nil {
		return 0, err
	}
	return mvf.reader.Seek(offset, whence)
}

// Close implements afero.File.Close
func (mvf *MetadataVirtualFile) Close() error {
	if mvf.reader != nil {
		return mvf.reader.Close()
	}
	return nil
}

// Name implements afero.File.Name
func (mvf *MetadataVirtualFile) Name() string {
	return mvf.name
}

// Readdir implements afero.File.Readdir
func (mvf *MetadataVirtualFile) Readdir(count int) ([]fs.FileInfo, error) {
	// For files, this should only work on directories
	// Since we're using simplified schema, we'll return an error for files
	return nil, fmt.Errorf("readdir not supported on files in simplified schema")
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
func (mvf *MetadataVirtualFile) Write(p []byte) (n int, error) {
	return 0, fmt.Errorf("write not supported")
}

// WriteAt implements afero.File.WriteAt (not supported)
func (mvf *MetadataVirtualFile) WriteAt(p []byte, off int64) (n int, error) {
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

// initReader initializes the underlying file reader for actual file content
func (mvf *MetadataVirtualFile) initReader() error {
	if mvf.readerInitialized {
		return nil
	}

	// Use global credentials for now
	var password, salt *string
	if mvf.globalPassword != "" {
		password = &mvf.globalPassword
	}
	if mvf.globalSalt != "" {
		salt = &mvf.globalSalt
	}

	// Create reader for the file using segments from metadata
	reader, err := mvf.createFileReader(mvf.fileMeta.SegmentData, password, salt)
	if err != nil {
		return fmt.Errorf("failed to create file reader: %w", err)
	}

	mvf.reader = reader
	mvf.readerInitialized = true
	return nil
}

// createFileReader creates a reader for files using the simplified schema
func (mvf *MetadataVirtualFile) createFileReader(segments []*metapb.SegmentData, password, salt *string) (afero.File, error) {
	// For now, create a simple implementation that returns an error
	// This will be implemented when we integrate with the usenet package
	return nil, fmt.Errorf("file reading not yet implemented for metadata filesystem")
}