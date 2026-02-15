package metadata

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// truncateFilename truncates the filename if it's too long to prevent filesystem issues
// when creating .meta files. Keeps filename under 250 characters.
func (ms *MetadataService) truncateFilename(filename string) string {
	fileExt := filepath.Ext(filename)
	filename = strings.TrimSuffix(filename, fileExt)

	const maxLen = 250 // Leave room for .meta extension

	if len(filename) <= maxLen {
		return filename + fileExt
	}

	// Simply truncate to maxLen
	return filename[:maxLen] + fileExt
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
	truncatedFilename := ms.truncateFilename(filename)
	metadataPath := filepath.Join(metadataDir, truncatedFilename+".meta")

	// Sidecar ID handling for compatibility
	// We don't write NzbdavId to the proto to maintain compatibility with versions that don't have field 14.
	// Instead, we store it in a sidecar .id file.
	nzbdavId := metadata.NzbdavId
	metadata.NzbdavId = "" // Clear for marshalling

	// Marshal protobuf data
	data, err := proto.Marshal(metadata)
	if err != nil {
		metadata.NzbdavId = nzbdavId // Restore on error
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Write directly to metadata file
	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		metadata.NzbdavId = nzbdavId // Restore on error
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	// Handle ID sidecar file
	idPath := metadataPath + ".id"
	if nzbdavId != "" {
		if err := os.WriteFile(idPath, []byte(nzbdavId), 0644); err != nil {
			// Log error but don't fail the operation
			slog.Warn("Failed to write ID sidecar file", "path", idPath, "error", err)
		}
	} else {
		// Clean up existing ID file if present
		_ = os.Remove(idPath)
	}

	metadata.NzbdavId = nzbdavId // Restore for in-memory use

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

	// Read ID from sidecar file (compatibility mode)
	idPath := metadataPath + ".id"
	if idData, err := os.ReadFile(idPath); err == nil {
		metadata.NzbdavId = string(idData)
	}

	return metadata, nil
}

// FileExists checks if a metadata file exists for the given virtual path
func (ms *MetadataService) FileExists(virtualPath string) bool {
	filename := filepath.Base(virtualPath)
	truncatedFilename := ms.truncateFilename(filename)
	metadataDir := filepath.Join(ms.rootPath, filepath.Dir(virtualPath))
	metadataPath := filepath.Join(metadataDir, truncatedFilename+".meta")

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
	aesKey []byte,
	aesIv []byte,
	releaseDate int64,
	par2Files []*metapb.Par2FileReference,
	nzbdavId string,
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
		AesKey:        aesKey,
		AesIv:         aesIv,
		CreatedAt:     now,
		ModifiedAt:    now,
		ReleaseDate:   releaseDate,
		Par2Files:     par2Files,
		NzbdavId:      nzbdavId,
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
	return ms.DeleteFileMetadataWithSourceNzb(context.Background(), virtualPath, false)
}

// DeleteFileMetadataWithSourceNzb deletes a metadata file and optionally its source NZB
func (ms *MetadataService) DeleteFileMetadataWithSourceNzb(ctx context.Context, virtualPath string, deleteSourceNzb bool) error {
	filename := filepath.Base(virtualPath)
	metadataDir := filepath.Join(ms.rootPath, filepath.Dir(virtualPath))
	metadataPath := filepath.Join(metadataDir, filename+".meta")

	// If we need to delete the source NZB, read the metadata first
	var sourceNzbPath string
	if deleteSourceNzb {
		metadata, err := ms.ReadFileMetadata(virtualPath)
		if err == nil && metadata != nil && metadata.SourceNzbPath != "" {
			sourceNzbPath = metadata.SourceNzbPath
		}
	}

	// Delete the metadata file
	err := os.Remove(metadataPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete metadata file: %w", err)
	}

	// Clean up .id sidecar file
	idPath := metadataPath + ".id"
	if removeErr := os.Remove(idPath); removeErr != nil && !os.IsNotExist(removeErr) {
		slog.DebugContext(ctx, "Failed to remove .id sidecar file", "path", idPath, "error", removeErr)
	}

	// Optionally delete the source NZB file (error-tolerant)
	if deleteSourceNzb && sourceNzbPath != "" {
		if err := os.Remove(sourceNzbPath); err != nil {
			if !os.IsNotExist(err) {
				slog.DebugContext(ctx, "Failed to delete source NZB file",
					"nzb_path", sourceNzbPath,
					"error", err)
			}
		} else {
			slog.DebugContext(ctx, "Deleted source NZB file",
				"nzb_path", sourceNzbPath,
				"virtual_path", virtualPath)
		}
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

// RenameFileMetadata atomically renames a metadata file (and its .id sidecar) from oldVirtualPath to newVirtualPath.
// Uses os.Rename for atomicity on the same filesystem, falling back to read-write-delete for cross-device moves.
func (ms *MetadataService) RenameFileMetadata(oldVirtualPath, newVirtualPath string) error {
	oldFilename := filepath.Base(oldVirtualPath)
	oldDir := filepath.Join(ms.rootPath, filepath.Dir(oldVirtualPath))
	oldMetaPath := filepath.Join(oldDir, oldFilename+".meta")

	newFilename := filepath.Base(newVirtualPath)
	newDir := filepath.Join(ms.rootPath, filepath.Dir(newVirtualPath))
	newMetaPath := filepath.Join(newDir, newFilename+".meta")

	// Ensure destination directory exists
	if err := os.MkdirAll(newDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination metadata directory: %w", err)
	}

	// Try atomic rename first
	if err := os.Rename(oldMetaPath, newMetaPath); err != nil {
		// Fall back to read-write-delete for cross-device moves
		if !isCrossDeviceError(err) {
			return fmt.Errorf("failed to rename metadata file: %w", err)
		}

		if err := copyAndRemoveFile(oldMetaPath, newMetaPath); err != nil {
			return fmt.Errorf("failed to copy metadata file across devices: %w", err)
		}
	}

	// Also rename the .id sidecar file if it exists
	oldIDPath := oldMetaPath + ".id"
	newIDPath := newMetaPath + ".id"
	if _, err := os.Stat(oldIDPath); err == nil {
		if err := os.Rename(oldIDPath, newIDPath); err != nil {
			// Cross-device fallback for .id file
			if isCrossDeviceError(err) {
				_ = copyAndRemoveFile(oldIDPath, newIDPath)
			} else {
				slog.Warn("Failed to rename .id sidecar file", "old", oldIDPath, "new", newIDPath, "error", err)
			}
		}
	}

	return nil
}

// UpdateIDSymlink creates or updates an ID-based symlink in the .ids/ sharded directory.
func (ms *MetadataService) UpdateIDSymlink(nzbdavID, virtualPath string) error {
	id := strings.ToLower(nzbdavID)
	if len(id) < 5 {
		return nil // Invalid ID for sharding
	}

	shardPath := filepath.Join(".ids", string(id[0]), string(id[1]), string(id[2]), string(id[3]), string(id[4]))
	fullShardDir := filepath.Join(ms.rootPath, shardPath)

	if err := os.MkdirAll(fullShardDir, 0755); err != nil {
		return err
	}

	targetMetaPath := ms.GetMetadataFilePath(virtualPath)
	linkPath := filepath.Join(fullShardDir, id+".meta")

	// Remove existing symlink if present
	os.Remove(linkPath)

	// Create relative symlink
	relTarget, err := filepath.Rel(fullShardDir, targetMetaPath)
	if err != nil {
		return os.Symlink(targetMetaPath, linkPath)
	}

	return os.Symlink(relTarget, linkPath)
}

// RemoveIDSymlink removes an ID-based symlink from the .ids/ sharded directory.
func (ms *MetadataService) RemoveIDSymlink(nzbdavID string) error {
	id := strings.ToLower(nzbdavID)
	if len(id) < 5 {
		return nil
	}

	shardPath := filepath.Join(".ids", string(id[0]), string(id[1]), string(id[2]), string(id[3]), string(id[4]))
	linkPath := filepath.Join(ms.rootPath, shardPath, id+".meta")

	if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// WalkDirectoryFiles walks a metadata directory and calls fn for each file's virtual path and metadata.
func (ms *MetadataService) WalkDirectoryFiles(virtualPath string, fn func(fileVirtualPath string, meta *metapb.FileMetadata) error) error {
	metadataDir := filepath.Join(ms.rootPath, virtualPath)

	return filepath.WalkDir(metadataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".meta") {
			return nil
		}

		relPath, err := filepath.Rel(ms.rootPath, path)
		if err != nil {
			return nil
		}
		fileVirtualPath := strings.TrimSuffix(relPath, ".meta")

		meta, err := ms.ReadFileMetadata(fileVirtualPath)
		if err != nil || meta == nil {
			return nil
		}

		return fn(fileVirtualPath, meta)
	})
}

// isCrossDeviceError checks if an error is a cross-device link error (EXDEV).
func isCrossDeviceError(err error) bool {
	return strings.Contains(err.Error(), "cross-device") || strings.Contains(err.Error(), "invalid cross-device link")
}

// copyAndRemoveFile copies src to dst then removes src.
func copyAndRemoveFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		os.Remove(dst) // Clean up partial write
		return err
	}

	if err := dstFile.Close(); err != nil {
		return err
	}
	srcFile.Close()

	return os.Remove(src)
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

func (ms *MetadataService) CreateDirectory(name string) error {
	return os.MkdirAll(filepath.Join(ms.rootPath, name), 0755)
}

func (ms *MetadataService) CreateDirectoryAll(name string) error {
	return os.MkdirAll(filepath.Join(ms.rootPath, name), 0755)
}

// CleanupEmptyDirectories recursively removes empty directories under the given virtual path.
// Uses a bottom-up approach to ensure parent directories are also removed if they become empty.
func (ms *MetadataService) CleanupEmptyDirectories(virtualPath string, protected []string) error {
	fullPath := filepath.Join(ms.rootPath, virtualPath)

	// Check if path exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return nil
	}

	return ms.cleanupEmptyDirsRecursive(fullPath, protected)
}

func (ms *MetadataService) cleanupEmptyDirsRecursive(path string, protected []string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	isEmpty := true
	for _, entry := range entries {
		if entry.IsDir() {
			subPath := filepath.Join(path, entry.Name())
			if err := ms.cleanupEmptyDirsRecursive(subPath, protected); err != nil {
				slog.Debug("Failed to cleanup sub-directory", "path", subPath, "error", err)
				isEmpty = false // Keep parent if sub-cleanup failed
				continue
			}

			// Re-check after sub-directory cleanup
			subEntries, _ := os.ReadDir(subPath)
			if len(subEntries) > 0 {
				isEmpty = false
			}
		} else {
			isEmpty = false
		}
	}

	// Don't delete the root of the cleanup
	if isEmpty && path != ms.rootPath && !ms.isCompleteDir(path) {
		// Check protected list
		base := filepath.Base(path)
		if strings.EqualFold(base, "corrupted_metadata") {
			return nil
		}

		for _, p := range protected {
			if strings.EqualFold(base, p) {
				return nil
			}
		}

		slog.Debug("Removing empty metadata directory", "path", path)
		return os.Remove(path)
	}

	return nil
}

// MoveToCorrupted moves a metadata file to a special corrupted directory for safety
func (ms *MetadataService) MoveToCorrupted(ctx context.Context, virtualPath string) error {
	// Normalize path and remove leading slashes to ensure it joins correctly
	cleanPath := filepath.FromSlash(strings.TrimPrefix(virtualPath, "/"))
	dir := filepath.Dir(cleanPath)
	filename := filepath.Base(cleanPath)

	truncatedFilename := ms.truncateFilename(filename)
	metadataPath := filepath.Join(ms.rootPath, dir, truncatedFilename+".meta")

	// Check if source exists
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return nil
	}

	// Define corrupted directory path (root/corrupted_metadata/...)
	// We use a visible folder name as requested.
	corruptedRoot := filepath.Join(ms.rootPath, "corrupted_metadata")
	targetDir := filepath.Join(corruptedRoot, dir)

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create corrupted metadata directory: %w", err)
	}

	targetPath := filepath.Join(targetDir, truncatedFilename+".meta")

	// Move the .meta file
	if err := os.Rename(metadataPath, targetPath); err != nil {
		slog.WarnContext(ctx, "Failed to move corrupted metadata, trying copy fallback", "error", err)
		// Rename can fail across different volumes, though usually metadata is on one volume.
		// For simplicity, we return the error here as it's unexpected for metadata.
		return err
	}

	// Also try to move the .id file if it exists
	idPath := metadataPath + ".id"
	if _, err := os.Stat(idPath); err == nil {
		_ = os.Rename(idPath, targetPath+".id")
	}

	slog.InfoContext(ctx, "Moved corrupted metadata to safety folder preserving structure",
		"original", metadataPath,
		"target", targetPath)
	return nil
}

func (ms *MetadataService) isCompleteDir(path string) bool {
	// Simple check to avoid deleting the 'complete' folder itself
	return filepath.Base(path) == "complete"
}
