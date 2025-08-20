package metadata

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"google.golang.org/protobuf/proto"
)

// MetadataService provides low-level read/write operations for metadata files
type MetadataService struct {
	rootPath string
}

// NewMetadataService creates a new metadata service
func NewMetadataService(rootPath string) *MetadataService {
	return &MetadataService{
		rootPath: rootPath,
	}
}

// WriteFileMetadata writes file metadata to disk
func (ms *MetadataService) WriteFileMetadata(virtualPath string, metadata *metapb.FileMetadata) error {
	// Ensure the directory exists
	metadataDir := filepath.Join(ms.rootPath, filepath.Dir(virtualPath))
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	// Create metadata file path (filename + .meta extension)
	filename := filepath.Base(virtualPath)
	metadataPath := filepath.Join(metadataDir, filename+".meta")

	// Marshal protobuf data
	data, err := proto.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Write to file
	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// ReadFileMetadata reads file metadata from disk
func (ms *MetadataService) ReadFileMetadata(virtualPath string) (*metapb.FileMetadata, error) {
	// Create metadata file path
	filename := filepath.Base(virtualPath)
	metadataDir := filepath.Join(ms.rootPath, filepath.Dir(virtualPath))
	metadataPath := filepath.Join(metadataDir, filename+".meta")

	// Read file
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // File not found
		}
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	// Unmarshal protobuf data
	metadata := &metapb.FileMetadata{}
	if err := proto.Unmarshal(data, metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return metadata, nil
}

// FileExists checks if a metadata file exists for the given virtual path
func (ms *MetadataService) FileExists(virtualPath string) bool {
	filename := filepath.Base(virtualPath)
	metadataDir := filepath.Join(ms.rootPath, filepath.Dir(virtualPath))
	metadataPath := filepath.Join(metadataDir, filename+".meta")

	_, err := os.Stat(metadataPath)
	return err == nil
}

// DirectoryExists checks if a metadata directory exists
func (ms *MetadataService) DirectoryExists(virtualPath string) bool {
	metadataDir := filepath.Join(ms.rootPath, virtualPath)
	info, err := os.Stat(metadataDir)
	return err == nil && info.IsDir()
}

// ListDirectory lists all metadata files in a directory
func (ms *MetadataService) ListDirectory(virtualPath string) ([]string, error) {
	metadataDir := filepath.Join(ms.rootPath, virtualPath)

	entries, err := os.ReadDir(metadataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil // Directory not found, return empty list
		}
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".meta" {
			// Remove .meta extension to get virtual filename
			virtualName := entry.Name()[:len(entry.Name())-5]
			files = append(files, virtualName)
		}
	}

	return files, nil
}

// ListSubdirectories lists all subdirectories in a metadata directory
func (ms *MetadataService) ListSubdirectories(virtualPath string) ([]string, error) {
	metadataDir := filepath.Join(ms.rootPath, virtualPath)

	entries, err := os.ReadDir(metadataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil // Directory not found, return empty list
		}
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}

	return dirs, nil
}

// CreateFileMetadata creates a new FileMetadata with basic fields
func (ms *MetadataService) CreateFileMetadata(
	fileSize int64,
	sourceNzbPath string,
	status metapb.FileStatus,
	segmentData []*metapb.SegmentData,
	encryption metapb.Encryption,
	password string,
	salt string,
) *metapb.FileMetadata {
	now := time.Now().Unix()

	return &metapb.FileMetadata{
		FileSize:      fileSize,
		SourceNzbPath: sourceNzbPath,
		Status:        status,
		Password:      password,
		Salt:          salt,
		Encryption:    encryption,
		SegmentData:   segmentData,
		CreatedAt:     now,
		ModifiedAt:    now,
	}
}

// UpdateFileMetadata updates the modified timestamp of metadata
func (ms *MetadataService) UpdateFileMetadata(virtualPath string, updateFunc func(*metapb.FileMetadata)) error {
	// Read existing metadata
	metadata, err := ms.ReadFileMetadata(virtualPath)
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}
	if metadata == nil {
		return fmt.Errorf("metadata not found for path: %s", virtualPath)
	}

	// Apply update function
	updateFunc(metadata)

	// Update modified timestamp
	metadata.ModifiedAt = time.Now().Unix()

	// Write back to disk
	return ms.WriteFileMetadata(virtualPath, metadata)
}

// UpdateFileStatus updates the status of a file in metadata
func (ms *MetadataService) UpdateFileStatus(virtualPath string, status metapb.FileStatus) error {
	return ms.UpdateFileMetadata(virtualPath, func(metadata *metapb.FileMetadata) {
		metadata.Status = status
	})
}

// DeleteFileMetadata deletes a metadata file
func (ms *MetadataService) DeleteFileMetadata(virtualPath string) error {
	filename := filepath.Base(virtualPath)
	metadataDir := filepath.Join(ms.rootPath, filepath.Dir(virtualPath))
	metadataPath := filepath.Join(metadataDir, filename+".meta")

	err := os.Remove(metadataPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete metadata file: %w", err)
	}

	return nil
}

// DeleteDirectory deletes a metadata directory and all its contents
func (ms *MetadataService) DeleteDirectory(virtualPath string) error {
	metadataDir := filepath.Join(ms.rootPath, virtualPath)

	err := os.RemoveAll(metadataDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete metadata directory: %w", err)
	}

	return nil
}

// ValidateSourceNzb validates that the source NZB file exists and matches metadata
func (ms *MetadataService) ValidateSourceNzb(metadata *metapb.FileMetadata) error {
	if metadata.SourceNzbPath == "" {
		return fmt.Errorf("source NZB path is empty")
	}

	// Check if source NZB file exists
	if _, err := os.Stat(metadata.SourceNzbPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source NZB file not found: %s", metadata.SourceNzbPath)
		}
		return fmt.Errorf("failed to stat source NZB file: %w", err)
	}

	return nil
}

// CalculateSegmentSize calculates the total size from segment data
func (ms *MetadataService) CalculateSegmentSize(segments []*metapb.SegmentData) int64 {
	var totalSize int64
	for _, segment := range segments {
		segmentSize := segment.EndOffset - segment.StartOffset
		if segmentSize > 0 {
			totalSize += segmentSize
		}
	}
	return totalSize
}

// GetMetadataFilePath returns the filesystem path for a metadata file
func (ms *MetadataService) GetMetadataFilePath(virtualPath string) string {
	filename := filepath.Base(virtualPath)
	metadataDir := filepath.Join(ms.rootPath, filepath.Dir(virtualPath))
	return filepath.Join(metadataDir, filename+".meta")
}

// GetMetadataDirectoryPath returns the filesystem path for a metadata directory
func (ms *MetadataService) GetMetadataDirectoryPath(virtualPath string) string {
	return filepath.Join(ms.rootPath, virtualPath)
}

// CreateSegmentData creates a new SegmentData with the given parameters
func (ms *MetadataService) CreateSegmentData(startOffset, endOffset int64, messageID string) *metapb.SegmentData {
	return &metapb.SegmentData{
		StartOffset: startOffset,
		EndOffset:   endOffset,
		Id:          messageID,
	}
}

// WalkMetadata walks through all metadata files in the filesystem
func (ms *MetadataService) WalkMetadata(walkFunc func(virtualPath string, metadata *metapb.FileMetadata) error) error {
	return filepath.WalkDir(ms.rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip if not a .meta file
		if d.IsDir() || filepath.Ext(d.Name()) != ".meta" {
			return nil
		}

		// Calculate virtual path
		relPath, err := filepath.Rel(ms.rootPath, path)
		if err != nil {
			return err
		}

		// Remove .meta extension and convert to virtual path
		virtualName := filepath.Base(relPath)[:len(filepath.Base(relPath))-5]
		virtualDir := filepath.Dir(relPath)
		if virtualDir == "." {
			virtualDir = "/"
		} else {
			virtualDir = "/" + filepath.ToSlash(virtualDir)
		}
		virtualPath := filepath.Join(virtualDir, virtualName)
		virtualPath = filepath.ToSlash(virtualPath)

		// Read metadata
		metadata, err := ms.ReadFileMetadata(virtualPath)
		if err != nil {
			return fmt.Errorf("failed to read metadata for %s: %w", virtualPath, err)
		}

		if metadata != nil {
			return walkFunc(virtualPath, metadata)
		}

		return nil
	})
}
