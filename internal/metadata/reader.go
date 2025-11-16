package metadata

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// MetadataReader provides read operations for the virtual filesystem
type MetadataReader struct {
	service *MetadataService
}

// NewMetadataReader creates a new metadata reader
func NewMetadataReader(service *MetadataService) *MetadataReader {
	return &MetadataReader{
		service: service,
	}
}

// ListDirectoryContents lists all contents in a virtual directory path
// Returns real directories as fs.FileInfo and virtual files as FileMetadata
func (mr *MetadataReader) ListDirectoryContents(virtualPath string) ([]fs.FileInfo, []*metapb.FileMetadata, error) {
	// Clean the path
	virtualPath = filepath.Clean(virtualPath)
	if virtualPath == "." {
		virtualPath = "/"
	}

	// Convert virtual path to metadata filesystem path
	metadataDir := filepath.Join(mr.service.GetMetadataDirectoryPath(virtualPath))

	// Single os.ReadDir call to get all entries
	entries, err := os.ReadDir(metadataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []fs.FileInfo{}, []*metapb.FileMetadata{}, nil
		}
		return nil, nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var dirs []fs.FileInfo
	var files []*metapb.FileMetadata

	for _, entry := range entries {
		if entry.IsDir() {
			// It's a real directory - get fs.FileInfo
			info, err := entry.Info()
			if err == nil {
				dirs = append(dirs, info)
			}
		} else if filepath.Ext(entry.Name()) == ".meta" {
			// It's a metadata file - read the FileMetadata
			virtualName := entry.Name()[:len(entry.Name())-5] // Remove .meta extension
			virtualFilePath := filepath.Join(virtualPath, virtualName)

			fileMeta, err := mr.service.ReadFileMetadata(virtualFilePath)
			if err == nil && fileMeta != nil {
				files = append(files, fileMeta)
			}
		}
		// Ignore other files (not directories or .meta files)
	}

	return dirs, files, nil
}

// GetDirectoryInfo gets information about a real directory using os.Stat
func (mr *MetadataReader) GetDirectoryInfo(virtualPath string) (fs.FileInfo, error) {
	metadataPath := mr.service.GetMetadataDirectoryPath(virtualPath)
	info, err := os.Stat(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("directory not found: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", virtualPath)
	}
	return info, nil
}

// GetFileMetadata gets metadata for a virtual file
func (mr *MetadataReader) GetFileMetadata(virtualPath string) (*metapb.FileMetadata, error) {
	return mr.service.ReadFileMetadata(virtualPath)
}

// PathExists checks if a virtual path exists
func (mr *MetadataReader) PathExists(virtualPath string) (bool, error) {
	// Check if it's a directory
	if mr.service.DirectoryExists(virtualPath) {
		return true, nil
	}

	// Check if it's a file
	if mr.service.FileExists(virtualPath) {
		return true, nil
	}

	return false, nil
}

// IsDirectory checks if a virtual path is a directory
func (mr *MetadataReader) IsDirectory(virtualPath string) (bool, error) {
	// Check if it's a directory
	if mr.service.DirectoryExists(virtualPath) {
		return true, nil
	}

	// Check if it's a file
	if mr.service.FileExists(virtualPath) {
		return false, nil
	}

	return false, fmt.Errorf("path does not exist: %s", virtualPath)
}

// GetFileSegments retrieves usenet segments for a virtual file
func (mr *MetadataReader) GetFileSegments(virtualPath string) ([]*metapb.SegmentData, error) {
	fileMeta, err := mr.service.ReadFileMetadata(virtualPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file metadata: %w", err)
	}

	if fileMeta == nil {
		return nil, fmt.Errorf("file not found: %s", virtualPath)
	}

	// Return the protobuf segments directly
	return fileMeta.SegmentData, nil
}

// GetMetadataService returns the underlying metadata service
func (mr *MetadataReader) GetMetadataService() *MetadataService {
	return mr.service
}
