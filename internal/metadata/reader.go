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

// ListDirectoryContents lists all contents in a virtual directory path.
// Returns real directories as fs.FileInfo. File metadata is no longer returned;
// callers should use MetadataService.ReadFileMetadataLite for lightweight listing
// or ReadFileMetadata when full segment data is needed.
func (mr *MetadataReader) ListDirectoryContents(virtualPath string) ([]fs.FileInfo, error) {
	virtualPath = filepath.Clean(virtualPath)
	if virtualPath == "." {
		virtualPath = "/"
	}

	metadataDir := mr.service.GetMetadataDirectoryPath(virtualPath)

	entries, err := os.ReadDir(metadataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []fs.FileInfo{}, nil
		}
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var dirs []fs.FileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			info, infoErr := entry.Info()
			if infoErr == nil {
				dirs = append(dirs, info)
			}
		}
	}

	return dirs, nil
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
