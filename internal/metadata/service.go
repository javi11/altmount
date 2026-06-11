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

	lru "github.com/hashicorp/golang-lru/v2"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/utils"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

const (
	// defaultMetadataCacheSize is the max number of file metadata entries to cache.
	defaultMetadataCacheSize = 4096
)

// FileMetadataLite holds the minimal metadata needed for directory listings.
// This avoids keeping full FileMetadata protos (with SegmentData, Par2Files, etc.)
// in memory just for Readdir.
type FileMetadataLite struct {
	FileSize   int64
	ModifiedAt int64
	Status     metapb.FileStatus
}

// MetadataService provides low-level read/write operations for metadata files.
//
// Only a lightweight metadata projection (liteCache) is kept in memory. The
// full FileMetadata proto — dominated by SegmentData/NestedSources slices
// holding thousands of message-ID strings — is never cached. Callers that need
// segments (Open, HealthChecker) re-read from disk each time; the proto then
// lives only for the duration of the open handle or the health check. This
// bounds steady-state memory at ~liteCache_entries × 40 bytes instead of the
// previous unbounded segment retention.
type MetadataService struct {
	rootPath string
	// liteCache caches lightweight metadata (size, modtime, status) used by
	// Readdir/Stat/Getattr, and populated as a side effect of ReadFileMetadata
	// so info-only callers still benefit.
	liteCache *lru.Cache[string, *FileMetadataLite]
}

// NewMetadataService creates a new metadata service
func NewMetadataService(rootPath string) *MetadataService {
	liteCache, _ := lru.New[string, *FileMetadataLite](defaultMetadataCacheSize)
	return &MetadataService{
		rootPath:  rootPath,
		liteCache: liteCache,
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

	// Write atomically using a uniquely-named temporary file so concurrent
	// writes to the same final path don't race on the same .tmp name.
	tmpFile, err := os.CreateTemp(metadataDir, "."+truncatedFilename+".*.tmp")
	if err != nil {
		metadata.NzbdavId = nzbdavId
		return fmt.Errorf("failed to create temporary metadata file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, writeErr := tmpFile.Write(data); writeErr != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		metadata.NzbdavId = nzbdavId
		return fmt.Errorf("failed to write temporary metadata file: %w", writeErr)
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		_ = os.Remove(tmpPath)
		metadata.NzbdavId = nzbdavId
		return fmt.Errorf("failed to close temporary metadata file: %w", closeErr)
	}

	if err := os.Rename(tmpPath, metadataPath); err != nil {
		_ = os.Remove(tmpPath)
		metadata.NzbdavId = nzbdavId
		return fmt.Errorf("failed to rename metadata file: %w", err)
	}

	metadata.NzbdavId = nzbdavId // Restore for in-memory use

	// Update only the lightweight cache; the full proto (with SegmentData) is
	// never cached to avoid long-term retention of segment strings.
	ms.liteCache.Add(virtualPath, &FileMetadataLite{
		FileSize:   metadata.FileSize,
		ModifiedAt: metadata.ModifiedAt,
		Status:     metadata.Status,
	})

	return nil
}

// ReadFileMetadata reads file metadata from disk. The full proto (including
// SegmentData and NestedSources) is returned to the caller but NOT cached —
// those slices dominate heap usage and must not be retained beyond the
// caller's handle. As a side effect, the lightweight projection is cached so
// subsequent Readdir/Stat calls are fast without a disk read.
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

	// Resolve shared_outer_source_index references on nested sources.
	// Files imported with the dedupe writer store outer segments once at
	// the FileMetadata level; we re-populate per-source slice headers
	// here so the rest of the read path is unaware of the difference.
	if err := ExpandSharedOuterSources(metadata); err != nil {
		return nil, fmt.Errorf("failed to expand shared outer sources: %w", err)
	}

	// Read ID from sidecar file (compatibility mode)
	idPath := metadataPath + ".id"
	if idData, err := os.ReadFile(idPath); err == nil {
		metadata.NzbdavId = string(idData)
	}

	// Populate only the lightweight cache — the full proto is never cached.
	ms.liteCache.Add(virtualPath, &FileMetadataLite{
		FileSize:   metadata.FileSize,
		ModifiedAt: metadata.ModifiedAt,
		Status:     metadata.Status,
	})

	return metadata, nil
}

// liteScanBytes is how much of a .meta file we read up front when serving a
// directory listing. The lite fields (file_size=1, status=3, modified_at=5)
// are all varints near the start of the proto; the only intervening field
// that can be large is source_nzb_path=2 (a string). 4 KiB is comfortable
// headroom — virtually every real-world .meta has all three within the first
// ~200 bytes. Avoids reading and unmarshalling the full proto (which can be
// MBs for files with many NestedSources or SegmentData entries — the exact
// pattern that caused a 7.94 GB allocation spike during FileBrowser
// recursive PROPFIND walks).
const liteScanBytes = 4096

// ReadFileMetadataLite reads only the lightweight fields (size, modtime, status)
// needed for directory listings. On cache miss it reads at most liteScanBytes
// from the .meta file and scans the proto wire format for the three lite
// fields, never instantiating the full FileMetadata proto or its
// NestedSources/SegmentData slices. Falls back to a full read in the rare
// case the partial buffer doesn't cover the lite fields.
func (ms *MetadataService) ReadFileMetadataLite(virtualPath string) (*FileMetadataLite, error) {
	// Check lite cache first
	if cached, ok := ms.liteCache.Get(virtualPath); ok {
		return cached, nil
	}

	// Cache miss — read the head of the file and scan wire-format fields.
	filename := filepath.Base(virtualPath)
	metadataDir := filepath.Join(ms.rootPath, filepath.Dir(virtualPath))
	metadataPath := filepath.Join(metadataDir, filename+".meta")

	f, err := os.Open(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open metadata file: %w", err)
	}
	defer f.Close()

	buf := make([]byte, liteScanBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, fmt.Errorf("failed to read metadata head: %w", err)
	}
	buf = buf[:n]

	lite, ok := parseLiteFields(buf)
	if !ok {
		// Lite fields not located within liteScanBytes (extreme/unusual
		// source_nzb_path length, future schema reordering, etc). Fall back
		// to the full read so the listing is correct even at the cost of
		// transient allocation.
		return ms.readFileMetadataLiteFull(virtualPath)
	}
	ms.liteCache.Add(virtualPath, lite)
	return lite, nil
}

// parseLiteFields walks proto wire format inside buf and extracts the lite
// fields without allocating a full FileMetadata struct. Returns (lite, true)
// once both file_size (field 1) and status (field 3) are seen — modified_at
// (field 5) is best-effort within the same buffer. Returns (nil, false) if
// the buffer is exhausted without the required fields, signalling the
// caller to fall back to a full read.
//
// Field numbers must match metadata.proto. Tested via TestReadFileMetadataLite_*
// in service_test.go.
func parseLiteFields(buf []byte) (*FileMetadataLite, bool) {
	var lite FileMetadataLite
	var sawFileSize, sawStatus bool
	for len(buf) > 0 {
		num, typ, tagLen := protowire.ConsumeTag(buf)
		if tagLen < 0 {
			return nil, false
		}
		buf = buf[tagLen:]
		switch num {
		case 1: // file_size int64 (varint)
			v, l := protowire.ConsumeVarint(buf)
			if l < 0 {
				return nil, false
			}
			lite.FileSize = int64(v)
			sawFileSize = true
			buf = buf[l:]
		case 3: // status FileStatus (varint enum)
			v, l := protowire.ConsumeVarint(buf)
			if l < 0 {
				return nil, false
			}
			lite.Status = metapb.FileStatus(v)
			sawStatus = true
			buf = buf[l:]
		case 5: // modified_at int64 (varint)
			v, l := protowire.ConsumeVarint(buf)
			if l < 0 {
				return nil, false
			}
			lite.ModifiedAt = int64(v)
			buf = buf[l:]
		default:
			l := protowire.ConsumeFieldValue(num, typ, buf)
			if l < 0 {
				return nil, false
			}
			buf = buf[l:]
		}
		// Early exit once required fields are captured. modified_at is
		// best-effort within the partial buffer; if it sits past
		// liteScanBytes it stays zero and the listing still renders.
		if sawFileSize && sawStatus && lite.ModifiedAt != 0 {
			return &lite, true
		}
	}
	if sawFileSize && sawStatus {
		return &lite, true
	}
	return nil, false
}

// readFileMetadataLiteFull is the legacy slow path: read the entire .meta
// file and unmarshal the full proto. Only used as a fallback when the
// partial-read scan in ReadFileMetadataLite fails to locate the lite
// fields within liteScanBytes.
func (ms *MetadataService) readFileMetadataLiteFull(virtualPath string) (*FileMetadataLite, error) {
	filename := filepath.Base(virtualPath)
	metadataDir := filepath.Join(ms.rootPath, filepath.Dir(virtualPath))
	metadataPath := filepath.Join(metadataDir, filename+".meta")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	metadata := &metapb.FileMetadata{}
	if err := proto.Unmarshal(data, metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	lite := &FileMetadataLite{
		FileSize:   metadata.FileSize,
		ModifiedAt: metadata.ModifiedAt,
		Status:     metadata.Status,
	}
	ms.liteCache.Add(virtualPath, lite)
	return lite, nil
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

// ListDirectoryAll returns both subdirectory fs.FileInfo entries and virtual
// file names from a single os.ReadDir call. This is used by Readdir to avoid
// two separate directory reads.
func (ms *MetadataService) ListDirectoryAll(virtualPath string) (dirs []fs.FileInfo, fileNames []string, err error) {
	metadataDir := filepath.Join(ms.rootPath, virtualPath)

	entries, err := os.ReadDir(metadataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			info, infoErr := entry.Info()
			if infoErr == nil {
				dirs = append(dirs, info)
			}
		} else if filepath.Ext(entry.Name()) == ".meta" {
			virtualName := entry.Name()[:len(entry.Name())-5]
			fileNames = append(fileNames, virtualName)
		}
	}
	return dirs, fileNames, nil
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
	ms.liteCache.Remove(virtualPath)

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

	// Clean up empty parent directories in metadata path
	utils.RemoveEmptyDirs(ms.rootPath, metadataDir)

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
	// Purge all cached entries under this directory
	prefix := virtualPath + string(filepath.Separator)
	for _, key := range ms.liteCache.Keys() {
		if key == virtualPath || strings.HasPrefix(key, prefix) {
			ms.liteCache.Remove(key)
		}
	}

	metadataDir := filepath.Join(ms.rootPath, virtualPath)

	// HARD SAFETY: Never delete the root metadata path
	cleanMetadataDir := filepath.Clean(metadataDir)
	if cleanMetadataDir == filepath.Clean(ms.rootPath) || cleanMetadataDir == "/" || cleanMetadataDir == "." {
		return fmt.Errorf("safety block: refusing to remove root metadata directory: %s", cleanMetadataDir)
	}

	err := os.RemoveAll(metadataDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete metadata directory: %w", err)
	}

	return nil
}

// RenameFileMetadata atomically renames a metadata file (and its .id sidecar) from oldVirtualPath to newVirtualPath.
// Uses os.Rename for atomicity on the same filesystem, falling back to read-write-delete for cross-device moves.
func (ms *MetadataService) RenameFileMetadata(oldVirtualPath, newVirtualPath string) error {
	ms.liteCache.Remove(oldVirtualPath)
	ms.liteCache.Remove(newVirtualPath)

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
				slog.WarnContext(context.Background(), "Failed to rename .id sidecar file", "old", oldIDPath, "new", newIDPath, "error", err)
			}
		}
	}

	return nil
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

func (ms *MetadataService) CreateDirectory(name string) error {
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
				slog.DebugContext(context.Background(), "Failed to cleanup sub-directory", "path", subPath, "error", err)
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

		slog.DebugContext(context.Background(), "Removing empty metadata directory", "path", path)
		return os.Remove(path)
	}

	return nil
}

// MoveToCorrupted moves a metadata file to a special corrupted directory for safety
func (ms *MetadataService) MoveToCorrupted(ctx context.Context, virtualPath string) error {
	ms.liteCache.Remove(virtualPath)

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

// CleanupOrphanedIDSymlinks walks the .ids/ directory and removes symlinks whose
// targets no longer exist. Empty shard directories are cleaned up afterwards.
// Returns the number of removed symlinks.
func (ms *MetadataService) CleanupOrphanedIDSymlinks(ctx context.Context) (int, error) {
	idsRoot := filepath.Join(ms.rootPath, ".ids")
	if _, err := os.Stat(idsRoot); os.IsNotExist(err) {
		return 0, nil
	}

	removed := 0
	err := filepath.WalkDir(idsRoot, func(path string, d fs.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // skip errors
		}
		if d.IsDir() {
			return nil
		}

		// Only process symlinks
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}

		// Check if the symlink target exists
		target, readErr := os.Readlink(path)
		if readErr != nil {
			return nil
		}

		// Make target absolute if relative
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}

		if _, statErr := os.Stat(target); os.IsNotExist(statErr) {
			if removeErr := os.Remove(path); removeErr == nil {
				removed++
			}
		}

		return nil
	})

	if err != nil {
		return removed, err
	}

	// Clean empty shard directories bottom-up
	utils.RemoveEmptyDirs(ms.rootPath, idsRoot)

	return removed, nil
}

func (ms *MetadataService) isCompleteDir(path string) bool {
	// Simple check to avoid deleting the 'complete' folder itself
	return filepath.Base(path) == "complete"
}
