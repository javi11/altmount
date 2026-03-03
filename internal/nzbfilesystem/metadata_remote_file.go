package nzbfilesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/encryption/aes"
	"github.com/javi11/altmount/internal/encryption/rclone"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pathutil"
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
	rcloneCipher     *rclone.RcloneCrypt // For rclone encryption/decryption
	aesCipher        *aes.AesCipher      // For AES encryption/decryption
	streamTracker    StreamTracker       // Stream tracker for monitoring active streams
	segmentStore     usenet.SegmentStore // Optional segment cache (nil = disabled)
}

// Configuration is now accessed dynamically through config.ConfigGetter
// No longer need a separate config struct

// NewMetadataRemoteFile creates a new metadata-based remote file handler
func NewMetadataRemoteFile(
	metadataService *metadata.MetadataService,
	healthRepository *database.HealthRepository,
	poolManager pool.Manager,
	configGetter config.ConfigGetter,
	streamTracker StreamTracker,
	segmentStore usenet.SegmentStore,
) *MetadataRemoteFile {
	// Initialize rclone cipher with global credentials for encrypted files
	cfg := configGetter()
	rcloneConfig := &encryption.Config{
		RclonePassword: cfg.RClone.Password, // Global password fallback
		RcloneSalt:     cfg.RClone.Salt,     // Global salt fallback
	}

	rcloneCipher, _ := rclone.NewRcloneCipher(rcloneConfig)

	// Initialize AES cipher for encrypted archives
	aesCipher := aes.NewAesCipher()

	return &MetadataRemoteFile{
		metadataService:  metadataService,
		healthRepository: healthRepository,
		poolManager:      poolManager,
		configGetter:     configGetter,
		rcloneCipher:     rcloneCipher,
		aesCipher:        aesCipher,
		streamTracker:    streamTracker,
		segmentStore:     segmentStore,
	}
}

// Helper methods to get dynamic config values
func (mrf *MetadataRemoteFile) getMaxPrefetch() int {
	return mrf.configGetter().Streaming.MaxPrefetch
}

func (mrf *MetadataRemoteFile) getGlobalPassword() string {
	return mrf.configGetter().RClone.Password
}

func (mrf *MetadataRemoteFile) getGlobalSalt() string {
	return mrf.configGetter().RClone.Salt
}

// OpenFile opens a virtual file backed by metadata
func (mrf *MetadataRemoteFile) OpenFile(ctx context.Context, name string) (bool, afero.File, error) {
	// Forbid COPY operations - nzbfilesystem is read-only
	if isCopy, ok := ctx.Value(utils.IsCopy).(bool); ok && isCopy {
		return false, nil, os.ErrPermission
	}

	// Normalize the path to handle trailing slashes consistently
	normalizedName := normalizePath(name)

	// Extract showCorrupted flag from context
	showCorrupted := false
	if sc, ok := ctx.Value(utils.ShowCorrupted).(bool); ok {
		showCorrupted = sc
	}

	// Force showCorrupted if we are inside the corrupted_metadata folder
	// normalizedName is clean and has no trailing slashes
	if strings.HasPrefix(normalizedName, "corrupted_metadata/") || normalizedName == "corrupted_metadata" {
		showCorrupted = true
	}

	// Check if this is a directory first
	if mrf.metadataService.DirectoryExists(normalizedName) {
		// Create a directory handle
		virtualDir := &MetadataVirtualDirectory{
			name:             name,
			normalizedPath:   normalizedName,
			metadataService:  mrf.metadataService,
			healthRepository: mrf.healthRepository,
			configGetter:     mrf.configGetter,
			showCorrupted:    showCorrupted,
		}
		return true, virtualDir, nil
	}

	// Check if this path exists as a file in our metadata
	exists := mrf.metadataService.FileExists(normalizedName)
	if !exists {
		// Check if it's a sharded ID path (.ids/...)
		if strings.HasPrefix(normalizedName, ".ids/") {
			// Resolve the ID path to the actual virtual path
			resolvedPath, err := mrf.resolveIDPath(normalizedName)
			if err == nil && resolvedPath != "" {
				// Continue with the resolved path
				normalizedName = resolvedPath
				exists = true
			}
		}

		if !exists {
			// Check if this could be a valid empty directory
			if mrf.isValidEmptyDirectory(normalizedName) {
				// Create a directory handle for empty directory
				virtualDir := &MetadataVirtualDirectory{
					name:             name,
					normalizedPath:   normalizedName,
					metadataService:  mrf.metadataService,
					healthRepository: mrf.healthRepository,
					configGetter:     mrf.configGetter,
					showCorrupted:    showCorrupted,
				}
				return true, virtualDir, nil
			}
			// Neither file nor directory found
			return false, nil, nil
		}
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
		return false, nil, &CorruptedFileError{
			TotalExpected: fileMeta.FileSize,
			UnderlyingErr: ErrMissmatchedSegments,
		}
	}

	// Extract max prefetch from context if available (overrides global config)
	maxPrefetch := mrf.getMaxPrefetch()

	if w, ok := ctx.Value(utils.MaxPrefetchKey).(int); ok && w > 0 {
		maxPrefetch = w
	}

	// Start tracking stream if tracker available
	streamID := ""
	if suppress, _ := ctx.Value(utils.SuppressStreamTrackingKey).(bool); suppress {
		// Stream tracking handled at caller level (e.g. FUSE Handle)
		streamID = ""
	} else if mrf.streamTracker != nil {
		// Check if we already have a stream ID in context
		if id, ok := ctx.Value(utils.StreamIDKey).(string); ok && id != "" {
			streamID = id
		} else if stream, ok := ctx.Value(utils.ActiveStreamKey).(*ActiveStream); ok {
			streamID = stream.ID
		} else {
			// Check for source and username in context
			source := "FUSE"
			if s, ok := ctx.Value(utils.StreamSourceKey).(string); ok && s != "" {
				source = s
			}

			userName := "FUSE"
			if u, ok := ctx.Value(utils.StreamUserNameKey).(string); ok && u != "" {
				userName = u
			}

			clientIP := ""
			if ip, ok := ctx.Value(utils.ClientIPKey).(string); ok {
				clientIP = ip
			}

			userAgent := ""
			if ua, ok := ctx.Value(utils.UserAgentKey).(string); ok {
				userAgent = ua
			}

			// Fallback to FUSE if no tracking info in context
			streamID = mrf.streamTracker.Add(normalizedName, source, userName, clientIP, userAgent, fileMeta.FileSize)
		}
	}

	// Create a metadata-based virtual file handle
	virtualFile := &MetadataVirtualFile{
		name:             name,
		fileMeta:         fileMeta,
		metadataService:  mrf.metadataService,
		healthRepository: mrf.healthRepository,
		configGetter:     mrf.configGetter,
		poolManager:      mrf.poolManager,
		ctx:              ctx,
		maxPrefetch:      maxPrefetch,
		rcloneCipher:     mrf.rcloneCipher,
		aesCipher:        mrf.aesCipher,
		globalPassword:   mrf.getGlobalPassword(),
		globalSalt:       mrf.getGlobalSalt(),
		streamTracker:    mrf.streamTracker,
		streamID:         streamID,
		segmentStore:     mrf.segmentStore,
	}

	return true, virtualFile, nil
}

// RemoveFile removes a virtual file or directory from the metadata
func (mrf *MetadataRemoteFile) RemoveFile(ctx context.Context, fileName string) (bool, error) {
	// Normalize the path to handle trailing slashes consistently
	normalizedName := normalizePath(fileName)

	// Prevent removal of root directory
	if normalizedName == RootPath {
		return false, ErrCannotRemoveRoot
	}

	// Prevent removal of category folders
	if mrf.isCategoryFolder(normalizedName) {
		slog.DebugContext(ctx, "Silently ignored removal request for category folder", "path", normalizedName)
		// Return true (success) but do nothing. This prevents Sonarr/Radarr/rclone
		// from logging "directory not empty" or "permission denied" errors.
		return true, nil
	}

	// Check if this is a directory
	if mrf.metadataService.DirectoryExists(normalizedName) {
		// Use MetadataService's directory delete operation
		return true, mrf.metadataService.DeleteDirectory(normalizedName)
	}

	// Check if this path exists as a file in our metadata
	exists := mrf.metadataService.FileExists(normalizedName)
	if !exists {
		// Neither file nor directory found in metadata
		return false, nil
	}

	// Try to find the physical path from health record for cleanup
	var physicalPath string
	if mrf.healthRepository != nil {
		if health, err := mrf.healthRepository.GetFileHealth(ctx, normalizedName); err == nil && health != nil {
			if health.LibraryPath != nil && *health.LibraryPath != "" {
				physicalPath = *health.LibraryPath
			}
		}
	}

	// Check if we should delete the source NZB file
	cfg := mrf.configGetter()
	deleteSourceNzb := cfg.Metadata.DeleteSourceNzbOnRemoval != nil && *cfg.Metadata.DeleteSourceNzbOnRemoval

	// Use MetadataService's file delete operation with optional NZB deletion
	err := mrf.metadataService.DeleteFileMetadataWithSourceNzb(ctx, normalizedName, deleteSourceNzb)
	if err != nil {
		return true, err
	}

	// Clean up empty physical directories if we found a physical path
	if physicalPath != "" {
		var rootPath string
		if cfg.Health.LibraryDir != nil && *cfg.Health.LibraryDir != "" {
			rootPath = *cfg.Health.LibraryDir
		} else {
			rootPath = cfg.MountPath
		}

		if rootPath != "" {
			pathutil.RemoveEmptyDirs(rootPath, filepath.Dir(physicalPath))
		}
	}

	return true, nil
}

// RenameFile renames a virtual file or directory in the metadata
func (mrf *MetadataRemoteFile) RenameFile(ctx context.Context, oldName, newName string) (bool, error) {
	// Normalize paths
	normalizedOld := normalizePath(oldName)
	normalizedNew := normalizePath(newName)

	slog.InfoContext(ctx, "MOVE operation requested", "source", normalizedOld, "destination", normalizedNew)

	// Prevent renaming of category folders
	if mrf.isCategoryFolder(normalizedOld) {
		slog.WarnContext(ctx, "Prevented renaming of category folder", "path", normalizedOld)
		return false, os.ErrPermission
	}

	// Check if old path is a directory
	if mrf.metadataService.DirectoryExists(normalizedOld) {
		// Get the filesystem paths for the directories
		oldDirPath := mrf.metadataService.GetMetadataDirectoryPath(normalizedOld)
		newDirPath := mrf.metadataService.GetMetadataDirectoryPath(normalizedNew)

		slog.InfoContext(ctx, "Moving metadata directory", "from", oldDirPath, "to", newDirPath)

		// Rename the entire directory
		if err := os.Rename(oldDirPath, newDirPath); err != nil {
			return false, fmt.Errorf("failed to rename directory: %w", err)
		}

		// Update health records for all files under the renamed directory
		if mrf.healthRepository != nil {
			if err := mrf.healthRepository.RenameHealthRecord(ctx, normalizedOld, normalizedNew); err != nil {
				slog.WarnContext(ctx, "Failed to update health records for renamed directory", "old", normalizedOld, "new", normalizedNew, "error", err)
			}
		}

		// Update ID symlinks for all files with NzbdavId under the renamed directory
		_ = mrf.metadataService.WalkDirectoryFiles(normalizedNew, func(fileVirtualPath string, meta *metapb.FileMetadata) error {
			if meta.NzbdavId != "" {
				if err := mrf.metadataService.UpdateIDSymlink(meta.NzbdavId, fileVirtualPath); err != nil {
					slog.WarnContext(ctx, "Failed to update ID symlink after directory rename", "id", meta.NzbdavId, "path", fileVirtualPath, "error", err)
				}
			}
			return nil
		})

		return true, nil
	}

	// Check if old path exists as a file
	exists := mrf.metadataService.FileExists(normalizedOld)
	if !exists {
		slog.WarnContext(ctx, "MOVE source not found", "path", normalizedOld)
		return false, nil
	}

	// Read metadata first to get NzbdavId before rename
	fileMeta, err := mrf.metadataService.ReadFileMetadata(normalizedOld)
	if err != nil {
		return false, fmt.Errorf("failed to read old metadata: %w", err)
	}

	// Use atomic rename instead of read-write-delete
	if err := mrf.metadataService.RenameFileMetadata(normalizedOld, normalizedNew); err != nil {
		return false, fmt.Errorf("failed to rename metadata: %w", err)
	}

	slog.InfoContext(ctx, "MOVE operation successful", "source", normalizedOld, "destination", normalizedNew)

	// Update ID symlink if file has a NzbdavId
	if fileMeta != nil && fileMeta.NzbdavId != "" {
		if err := mrf.metadataService.UpdateIDSymlink(fileMeta.NzbdavId, normalizedNew); err != nil {
			slog.WarnContext(ctx, "Failed to update ID symlink during MOVE", "id", fileMeta.NzbdavId, "error", err)
		}
	}

	// Update health records
	if mrf.healthRepository != nil {
		if err := mrf.healthRepository.RenameHealthRecord(ctx, normalizedOld, normalizedNew); err != nil {
			slog.WarnContext(ctx, "Failed to update health record path during MOVE", "old", normalizedOld, "new", normalizedNew, "error", err)
		}

		// Check if we should resolve other repairs in the same directory
		cfg := mrf.configGetter()
		resolveRepairs := true
		if cfg.Health.ResolveRepairOnImport != nil {
			resolveRepairs = *cfg.Health.ResolveRepairOnImport
		}

		if resolveRepairs {
			parentDir := filepath.Dir(normalizedNew)
			if parentDir != "." && parentDir != "/" {
				if count, err := mrf.healthRepository.ResolvePendingRepairsInDirectory(ctx, parentDir); err == nil && count > 0 {
					slog.InfoContext(ctx, "Resolved pending repairs in directory due to MOVE operation",
						"directory", parentDir,
						"resolved_count", count)
				}
			}
		}
	}

	return true, nil
}

// isCategoryFolder checks if a path corresponds to a configured category folder
func (mrf *MetadataRemoteFile) isCategoryFolder(path string) bool {
	cfg := mrf.configGetter()
	normalizedPath := strings.Trim(normalizePath(path), "/")
	completeDir := strings.Trim(normalizePath(cfg.SABnzbd.CompleteDir), "/")

	// Helper to check if a name matches a category
	matchesCategory := func(name string) bool {
		name = strings.Trim(normalizePath(name), "/")
		if name == "" {
			return false
		}

		// Check exact match
		if normalizedPath == name {
			return true
		}

		// Check match with complete_dir prefix (e.g. complete/tv)
		if completeDir != "" && normalizedPath == strings.Trim(completeDir+"/"+name, "/") {
			return true
		}

		return false
	}

	// Check complete_dir itself
	if normalizedPath == completeDir {
		return true
	}

	// Check configured categories
	for _, cat := range cfg.SABnzbd.Categories {
		// Check both the category name and its specific directory if set
		if matchesCategory(cat.Name) {
			return true
		}
		if cat.Dir != "" && matchesCategory(cat.Dir) {
			return true
		}
	}

	return false
}

// Stat returns file information for a path using metadata
func (mrf *MetadataRemoteFile) Stat(ctx context.Context, name string) (bool, fs.FileInfo, error) {
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
		// Check if it's a sharded ID path (.ids/...)
		if strings.HasPrefix(normalizedName, ".ids/") {
			// Resolve the ID path to the actual virtual path
			resolvedPath, err := mrf.resolveIDPath(normalizedName)
			if err == nil && resolvedPath != "" {
				// Continue with the resolved path
				normalizedName = resolvedPath
				exists = true
			}
		}

		if !exists {
			return false, nil, fs.ErrNotExist
		}
	}

	// Get file metadata using simplified schema
	fileMeta, err := mrf.metadataService.ReadFileMetadata(normalizedName)
	if err != nil {
		return false, nil, fmt.Errorf("failed to read file metadata: %w", err)
	}

	if fileMeta == nil {
		return false, nil, fs.ErrNotExist
	}

	// Extract showCorrupted flag from context
	showCorrupted := false
	if sc, ok := ctx.Value(utils.ShowCorrupted).(bool); ok {
		showCorrupted = sc
	}

	// Filter out masked files if masking is enabled and not showing corrupted
	if !showCorrupted {
		cfg := mrf.configGetter()
		if cfg.Streaming.FailureMasking.Enabled == nil || *cfg.Streaming.FailureMasking.Enabled {
			health, err := mrf.healthRepository.GetFileHealth(ctx, normalizedName)
			if err == nil && health != nil && health.IsMasked {
				return false, nil, fs.ErrNotExist
			}
		}
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
func (mfi *MetadataFileInfo) Sys() any           { return nil }

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
		Size:  seg.SegmentSize,
	}, []string{}, true // Empty groups for now - could be stored in metadata if needed
}

// MetadataVirtualDirectory implements afero.File for metadata-backed virtual directories
type MetadataVirtualDirectory struct {
	name             string
	normalizedPath   string
	metadataService  *metadata.MetadataService
	healthRepository *database.HealthRepository
	configGetter     config.ConfigGetter
	showCorrupted    bool
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

	// Check if failure masking is enabled
	cfg := mvd.configGetter()
	maskingEnabled := cfg.Streaming.FailureMasking.Enabled == nil || *cfg.Streaming.FailureMasking.Enabled

	ctx := context.Background()

	for _, fileName := range fileNames {
		virtualFilePath := filepath.Join(mvd.normalizedPath, fileName)
		fileMeta, err := mvd.metadataService.ReadFileMetadata(virtualFilePath)
		if err != nil || fileMeta == nil {
			continue
		}

		// Skip corrupted files unless showCorrupted flag is set
		if !mvd.showCorrupted && fileMeta.Status == metapb.FileStatus_FILE_STATUS_CORRUPTED {
			continue
		}

		// Skip masked files if masking is enabled
		if maskingEnabled && !mvd.showCorrupted {
			health, err := mvd.healthRepository.GetFileHealth(ctx, virtualFilePath)
			if err == nil && health != nil && health.IsMasked {
				continue
			}
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
	configGetter     config.ConfigGetter
	poolManager      pool.Manager // Pool manager for dynamic pool access
	ctx              context.Context
	maxPrefetch      int // Maximum segments prefetched ahead of current read position
	rcloneCipher     *rclone.RcloneCrypt
	aesCipher        *aes.AesCipher
	globalPassword   string
	globalSalt       string
	streamTracker    StreamTracker
	streamID         string
	segmentStore     usenet.SegmentStore // optional segment cache
	segmentIndexOnce sync.Once           // guards lazy init of segmentIndex

	// Reader state and position tracking
	reader            io.ReadCloser
	readerInitialized bool
	position          int64 // File position (what client sees after Seek)
	currentRangeStart int64 // Start of current reader's range
	currentRangeEnd   int64 // End of current reader's range
	originalRangeEnd  int64 // Original end requested by client (-1 for unbounded)

	// Segment offset index for O(1) offset→segment lookup
	segmentIndex *segmentOffsetIndex

	mu      sync.Mutex
	closeWg sync.WaitGroup // tracks background reader closes during seek
}

// segmentOffsetIndex provides O(1) lookup for offset→segment mapping using binary search
type segmentOffsetIndex struct {
	offsets []int64 // Cumulative start offset of each segment in file coordinates
	sizes   []int64 // Size of each segment's usable data
}

// buildSegmentIndex builds an offset index from metadata segments for O(1) lookup
func buildSegmentIndex(segments []*metapb.SegmentData) *segmentOffsetIndex {
	if len(segments) == 0 {
		return nil
	}

	idx := &segmentOffsetIndex{
		offsets: make([]int64, len(segments)),
		sizes:   make([]int64, len(segments)),
	}

	var pos int64
	for i, seg := range segments {
		idx.offsets[i] = pos
		usableLen := seg.EndOffset - seg.StartOffset + 1
		idx.sizes[i] = usableLen
		pos += usableLen
	}

	return idx
}

// findSegmentForOffset returns the segment index containing the given file offset
// Returns -1 if offset is beyond all segments
func (idx *segmentOffsetIndex) findSegmentForOffset(offset int64) int {
	if idx == nil || len(idx.offsets) == 0 || offset < 0 {
		return -1
	}

	// Binary search for the segment containing this offset
	// We want the largest i such that offsets[i] <= offset
	n := len(idx.offsets)

	// Quick check: if offset is before first segment or beyond last
	if offset < idx.offsets[0] {
		return 0
	}

	lastSegEnd := idx.offsets[n-1] + idx.sizes[n-1] - 1
	if offset > lastSegEnd {
		return -1
	}

	// Binary search
	lo, hi := 0, n
	for lo < hi {
		mid := (lo + hi) / 2
		if idx.offsets[mid] <= offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	// lo-1 is the largest index where offsets[i] <= offset
	return lo - 1
}

// getOffsetForSegment returns the cumulative file offset at the start of the given segment index
// Returns 0 if the index is invalid or out of bounds
func (idx *segmentOffsetIndex) getOffsetForSegment(segmentIndex int) int64 {
	if idx == nil || segmentIndex < 0 || segmentIndex >= len(idx.offsets) {
		return 0
	}
	return idx.offsets[segmentIndex]
}

// GetStreamID returns the active stream ID associated with this file handle
func (mvf *MetadataVirtualFile) GetStreamID() string {
	return mvf.streamID
}

// WarmUp triggers a background pre-fetch of the file start
func (mvf *MetadataVirtualFile) WarmUp() {
	go func() {
		mvf.mu.Lock()
		defer mvf.mu.Unlock()

		// Skip if already initialized
		if mvf.readerInitialized {
			return
		}

		// Initialize reader for the beginning of the file
		if err := mvf.ensureReader(); err != nil {
			// Just log/ignore, the actual Read will handle it later
			return
		}

		// If the reader supports manual starting (UsenetReader), trigger it
		// This starts the background workers to fetch data into the cache
		// without consuming any bytes from the stream.
		if ur, ok := mvf.reader.(interface{ Start() }); ok {
			ur.Start()
		}
	}()
}

// Read implements afero.File.Read
func (mvf *MetadataVirtualFile) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	mvf.mu.Lock()
	defer mvf.mu.Unlock()

	for n < len(p) {
		if err := mvf.ensureReader(); err != nil {
			return n, err
		}

		totalRead, readErr := mvf.reader.Read(p[n:])
		n += totalRead
		mvf.position += int64(totalRead)

		if totalRead > 0 && mvf.streamTracker != nil && mvf.streamID != "" {
			mvf.streamTracker.UpdateProgress(mvf.streamID, int64(totalRead))
			mvf.streamTracker.UpdateCurrentOffset(mvf.streamID, mvf.position)

			// Update buffered offset if available
			if ur, ok := mvf.reader.(interface{ GetBufferedOffset() int64 }); ok {
				mvf.streamTracker.UpdateBufferedOffset(mvf.streamID, ur.GetBufferedOffset())
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) && mvf.hasMoreDataToRead() {
				// Close current reader and try to get a new one for the next range in next iteration
				mvf.closeCurrentReader()
				continue
			}

			// For data corruption errors, report and mark as corrupted
			var dataCorruptionErr *usenet.DataCorruptionError
			if errors.As(readErr, &dataCorruptionErr) {
				mvf.updateFileHealthOnError(dataCorruptionErr, dataCorruptionErr.NoRetry)
				return n, &CorruptedFileError{
					TotalExpected: mvf.fileMeta.FileSize,
					UnderlyingErr: dataCorruptionErr,
				}
			}

			return n, readErr
		}
	}

	return n, nil
}

// ReadAt implements afero.File.ReadAt with concurrent random access support.
// Unlike Read(), this method creates an independent reader for each call,
// allowing concurrent reads at different offsets without mutex serialization.
func (mvf *MetadataVirtualFile) ReadAt(p []byte, off int64) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	// Validate offset bounds
	if off < 0 {
		return 0, ErrNegativeOffset
	}
	if off >= mvf.fileMeta.FileSize {
		return 0, io.EOF
	}

	// Calculate end position (don't read beyond file size)
	end := off + int64(len(p)) - 1
	if end >= mvf.fileMeta.FileSize {
		end = mvf.fileMeta.FileSize - 1
	}

	// Create an independent reader for this specific offset range
	// This reader is self-contained and doesn't affect the file's main position
	reader, err := mvf.createReaderAtOffset(off, end)
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	// Read the requested data using a context-aware wrapper.
	// This ensures reads are cancelled if the FUSE context expires,
	// even if the underlying reader blocks.
	ctx := mvf.ctx
	buf := p[:end-off+1]
	n, err = readFullContext(ctx, reader, buf)
	if err == io.ErrUnexpectedEOF {
		// Partial read is acceptable for ReadAt at end of file
		err = nil
	}

	return n, err
}

// createReaderAtOffset creates an independent reader for reading at a specific offset.
// This reader is self-contained and can be used concurrently with other readers.
func (mvf *MetadataVirtualFile) createReaderAtOffset(start, end int64) (io.ReadCloser, error) {
	if mvf.poolManager == nil {
		return nil, ErrNoUsenetPool
	}

	// Nested sources take priority — each source has its own segments and AES credentials
	if len(mvf.fileMeta.NestedSources) > 0 {
		return mvf.createNestedReader(start, end)
	}

	if len(mvf.fileMeta.SegmentData) == 0 {
		return nil, ErrMissmatchedSegments
	}

	// Create reader based on encryption type
	if mvf.fileMeta.Encryption != metapb.Encryption_NONE {
		return mvf.createEncryptedReaderAtOffset(start, end)
	}

	return mvf.createUsenetReader(mvf.ctx, start, end)
}

// createEncryptedReaderAtOffset creates an encrypted reader for a specific offset range
func (mvf *MetadataVirtualFile) createEncryptedReaderAtOffset(start, end int64) (io.ReadCloser, error) {
	switch mvf.fileMeta.Encryption {
	case metapb.Encryption_RCLONE:
		if mvf.rcloneCipher == nil {
			return nil, ErrNoCipherConfig
		}

		password := mvf.fileMeta.Password
		if password == "" {
			password = mvf.globalPassword
		}
		salt := mvf.fileMeta.Salt
		if salt == "" {
			salt = mvf.globalSalt
		}

		return mvf.rcloneCipher.Open(
			mvf.ctx,
			&utils.RangeHeader{Start: start, End: end},
			mvf.fileMeta.FileSize,
			password,
			salt,
			func(ctx context.Context, s, e int64) (io.ReadCloser, error) {
				return mvf.createUsenetReader(ctx, s, e)
			},
		)

	case metapb.Encryption_AES:
		if mvf.aesCipher == nil {
			return nil, ErrNoCipherConfig
		}
		if len(mvf.fileMeta.AesKey) == 0 {
			return nil, fmt.Errorf("missing AES key in metadata")
		}
		if len(mvf.fileMeta.AesIv) == 0 {
			return nil, fmt.Errorf("missing AES IV in metadata")
		}

		return mvf.aesCipher.Open(
			mvf.ctx,
			&utils.RangeHeader{Start: start, End: end},
			mvf.fileMeta.FileSize,
			mvf.fileMeta.AesKey,
			mvf.fileMeta.AesIv,
			func(ctx context.Context, s, e int64) (io.ReadCloser, error) {
				return mvf.createUsenetReader(ctx, s, e)
			},
		)

	default:
		return nil, fmt.Errorf("unsupported encryption type: %v", mvf.fileMeta.Encryption)
	}
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

	// Close reader if position changes - UsenetReader is forward-only and cannot seek.
	// Creating a new reader at the target position is faster than downloading and
	// discarding data to catch up.
	if mvf.readerInitialized && abs != mvf.position {
		mvf.closeCurrentReader()
	}

	// Reset originalRangeEnd when position changes to force fresh range calculation
	// on next read. This prevents stale range information from being reused after seek.
	if abs != mvf.position {
		mvf.originalRangeEnd = 0
		if mvf.streamTracker != nil && mvf.streamID != "" {
			mvf.streamTracker.UpdateCurrentOffset(mvf.streamID, abs)
		}
	}

	mvf.position = abs
	return abs, nil
}

// Close implements afero.File.Close
func (mvf *MetadataVirtualFile) Close() error {
	// Remove from stream tracker if applicable
	if mvf.streamTracker != nil && mvf.streamID != "" {
		mvf.streamTracker.Remove(mvf.streamID)
		mvf.streamID = ""
	}

	mvf.mu.Lock()
	if mvf.reader != nil {
		mvf.reader.Close()
		mvf.reader = nil
		mvf.readerInitialized = false
	}
	mvf.mu.Unlock()

	// Wait for any background reader closes from previous seeks
	mvf.closeWg.Wait()

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

// closeCurrentReader detaches the current reader and closes it in the background.
// This avoids blocking Seek on UsenetReader.Close() which may wait for in-flight downloads.
func (mvf *MetadataVirtualFile) closeCurrentReader() {
	if mvf.reader != nil {
		reader := mvf.reader
		mvf.reader = nil
		mvf.closeWg.Go(func() {
			reader.Close()
		})
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

	if end == -1 {
		end = mvf.fileMeta.FileSize - 1
	}

	// Track the current reader's range for progressive reading
	mvf.currentRangeStart = start
	mvf.currentRangeEnd = end

	// Create reader for the calculated range using metadata segments
	if len(mvf.fileMeta.NestedSources) > 0 {
		// Nested RAR: use multi-source reader
		reader, err := mvf.createNestedReader(start, end)
		if err != nil {
			return fmt.Errorf("failed to create nested reader: %w", err)
		}
		mvf.reader = reader
	} else if mvf.fileMeta.Encryption != metapb.Encryption_NONE {
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
		// Extract range from context
		if rangeStr, ok := mvf.ctx.Value(utils.RangeKey).(string); ok && rangeStr != "" {
			rangeHeader, err := utils.ParseRangeHeader(rangeStr)
			if err == nil && rangeHeader != nil {
				mvf.originalRangeEnd = rangeHeader.End
				return rangeHeader.Start, rangeHeader.End
			}
		}

		// No range header, set unbounded
		mvf.originalRangeEnd = -1
		return mvf.position, -1
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

	return mvf.position, targetEnd
}

// createUsenetReader creates a new usenet reader for the specified range using metadata segments
func (mvf *MetadataVirtualFile) createUsenetReader(ctx context.Context, start, end int64) (io.ReadCloser, error) {
	if len(mvf.fileMeta.SegmentData) == 0 {
		return nil, ErrMissmatchedSegments
	}

	// Build segment offset index lazily on first read (thread-safe via sync.Once)
	mvf.segmentIndexOnce.Do(func() {
		mvf.segmentIndex = buildSegmentIndex(mvf.fileMeta.SegmentData)
	})

	loader := newMetadataSegmentLoader(mvf.fileMeta.SegmentData)

	// segmentIndex is always non-nil here (built by segmentIndexOnce.Do above).
	// Use O(log n) binary search to find segment boundaries, then create a lazy
	// range with O(1) initialization. Corrupt metadata (index returning -1) results
	// in an empty range caught by HasSegments() below.
	startSegIdx := mvf.segmentIndex.findSegmentForOffset(start)
	startFilePos := mvf.segmentIndex.getOffsetForSegment(startSegIdx)
	endSegIdx := mvf.segmentIndex.findSegmentForOffset(end)
	endFilePos := mvf.segmentIndex.getOffsetForSegment(endSegIdx)

	rg := usenet.NewLazySegmentRange(ctx, start, end, loader, startSegIdx, startFilePos, endSegIdx, endFilePos)

	if !rg.HasSegments() {
		var availableBytes int64
		for _, seg := range mvf.fileMeta.SegmentData {
			availableBytes += seg.SegmentSize
		}

		slog.ErrorContext(ctx, "[createUsenetReader] No segments to download",
			"start", start,
			"end", end,
			"available_bytes", availableBytes,
			"expected_file_size", mvf.fileMeta.FileSize,
		)

		mvf.updateFileHealthOnError(&usenet.DataCorruptionError{
			UnderlyingErr: ErrMissmatchedSegments,
		}, true)

		return nil, &CorruptedFileError{
			TotalExpected: mvf.fileMeta.FileSize,
			UnderlyingErr: ErrMissmatchedSegments,
		}
	}

	return usenet.NewUsenetReader(ctx, mvf.poolManager.GetPool, rg, mvf.maxPrefetch, mvf.streamTracker, mvf.streamID, mvf.segmentStore)
}

// createNestedReader creates a reader for files backed by nested RAR sources.
// It maps the requested byte range [start, end] across multiple NestedSegmentSources,
// building a lazy reader that opens each inner-volume reader only when needed.
// This avoids opening all inner volumes simultaneously, which would cause all their
// segments to be prefetched concurrently and spike memory usage.
func (mvf *MetadataVirtualFile) createNestedReader(start, end int64) (io.ReadCloser, error) {
	sources := mvf.fileMeta.NestedSources
	if len(sources) == 0 {
		return nil, fmt.Errorf("no nested sources available")
	}

	// Calculate which sources contain the requested byte range.
	// Sources are concatenated: source 0 covers [0, InnerLength0),
	// source 1 covers [InnerLength0, InnerLength0+InnerLength1), etc.
	var specs []nestedSourceSpec
	var sourceOffset int64

	for _, src := range sources {
		srcEnd := sourceOffset + src.InnerLength - 1

		// Skip sources before our range
		if srcEnd < start {
			sourceOffset += src.InnerLength
			continue
		}

		// Stop if we've passed our range
		if sourceOffset > end {
			break
		}

		// Calculate local offsets within this source
		localStart := int64(0)
		if start > sourceOffset {
			localStart = start - sourceOffset
		}
		localEnd := src.InnerLength - 1
		if end < srcEnd {
			localEnd = end - sourceOffset
		}

		readLen := localEnd - localStart + 1
		if readLen <= 0 {
			sourceOffset += src.InnerLength
			continue
		}

		specs = append(specs, nestedSourceSpec{src: src, localStart: localStart, readLen: readLen})
		sourceOffset += src.InnerLength
	}

	if len(specs) == 0 {
		return nil, fmt.Errorf("no nested sources cover range [%d, %d]", start, end)
	}

	return &lazyNestedMultiReader{mvf: mvf, specs: specs}, nil
}

// createNestedSourceReader creates a reader for a single NestedSegmentSource,
// starting at innerStart within the decrypted inner volume and reading readLen bytes.
func (mvf *MetadataVirtualFile) createNestedSourceReader(
	src *metapb.NestedSegmentSource,
	innerStart int64,
	readLen int64,
) (io.ReadCloser, error) {
	absoluteStart := src.InnerOffset + innerStart

	if len(src.AesKey) > 0 {
		// Encrypted source: decrypt with AES-CBC then read at inner offset
		if mvf.aesCipher == nil {
			return nil, ErrNoCipherConfig
		}

		rh := &utils.RangeHeader{
			Start: absoluteStart,
			End:   absoluteStart + readLen - 1,
		}

		return mvf.aesCipher.Open(
			mvf.ctx,
			rh,
			src.InnerVolumeSize,
			src.AesKey,
			src.AesIv,
			func(ctx context.Context, s, e int64) (io.ReadCloser, error) {
				return mvf.createUsenetReaderFromSegments(ctx, src.Segments, s, e)
			},
		)
	}

	// Unencrypted source: read directly from segments at inner offset
	return mvf.createUsenetReaderFromSegments(mvf.ctx, src.Segments, absoluteStart, absoluteStart+readLen-1)
}

// createUsenetReaderFromSegments creates a usenet reader from a specific set of segments
// (used for nested source reading where segments differ from the main file metadata).
func (mvf *MetadataVirtualFile) createUsenetReaderFromSegments(ctx context.Context, segments []*metapb.SegmentData, start, end int64) (io.ReadCloser, error) {
	if len(segments) == 0 {
		return nil, ErrMissmatchedSegments
	}

	loader := newMetadataSegmentLoader(segments)
	idx := buildSegmentIndex(segments)

	startSegIdx := idx.findSegmentForOffset(start)
	startFilePos := idx.getOffsetForSegment(startSegIdx)
	endSegIdx := idx.findSegmentForOffset(end)
	endFilePos := idx.getOffsetForSegment(endSegIdx)

	rg := usenet.NewLazySegmentRange(ctx, start, end, loader, startSegIdx, startFilePos, endSegIdx, endFilePos)

	if !rg.HasSegments() {
		return nil, fmt.Errorf("no segments cover range [%d, %d]", start, end)
	}

	return usenet.NewUsenetReader(ctx, mvf.poolManager.GetPool, rg, mvf.maxPrefetch, mvf.streamTracker, mvf.streamID, mvf.segmentStore)
}

// nestedSourceSpec holds the parameters needed to lazily open one inner-volume reader.
type nestedSourceSpec struct {
	src        *metapb.NestedSegmentSource
	localStart int64
	readLen    int64
}

// lazyNestedMultiReader opens inner-volume readers one at a time, only when needed.
// This prevents all inner volumes from being opened simultaneously, which would cause
// all their segments to be prefetched concurrently and spike memory usage.
type lazyNestedMultiReader struct {
	mvf     *MetadataVirtualFile
	specs   []nestedSourceSpec
	idx     int
	current io.ReadCloser
}

func (r *lazyNestedMultiReader) Read(p []byte) (int, error) {
	for {
		if r.current == nil {
			if r.idx >= len(r.specs) {
				return 0, io.EOF
			}
			spec := r.specs[r.idx]
			rc, err := r.mvf.createNestedSourceReader(spec.src, spec.localStart, spec.readLen)
			if err != nil {
				return 0, err
			}
			r.current = rc
			r.idx++
		}

		n, err := r.current.Read(p)
		if err == io.EOF {
			r.current.Close()
			r.current = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (r *lazyNestedMultiReader) Close() error {
	if r.current != nil {
		err := r.current.Close()
		r.current = nil
		return err
	}
	return nil
}

// wrapWithEncryption wraps a usenet reader with encryption using metadata
func (mvf *MetadataVirtualFile) wrapWithEncryption(start, end int64) (io.ReadCloser, error) {
	if mvf.fileMeta.Encryption == metapb.Encryption_NONE {
		return nil, ErrNoEncryptionParams
	}

	switch mvf.fileMeta.Encryption {
	case metapb.Encryption_RCLONE:
		if mvf.rcloneCipher == nil {
			return nil, ErrNoCipherConfig
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

		// Wrap with rclone decryption
		decryptedReader, err := mvf.rcloneCipher.Open(
			mvf.ctx,
			&utils.RangeHeader{Start: start, End: end},
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

	case metapb.Encryption_AES:
		// AES encryption for RAR archives
		if mvf.aesCipher == nil {
			return nil, ErrNoCipherConfig
		}
		if len(mvf.fileMeta.AesKey) == 0 {
			return nil, fmt.Errorf("missing AES key in metadata")
		}
		if len(mvf.fileMeta.AesIv) == 0 {
			return nil, fmt.Errorf("missing AES IV in metadata")
		}

		// Wrap with AES decryption - pass key and IV directly
		decryptedReader, err := mvf.aesCipher.Open(
			mvf.ctx,
			&utils.RangeHeader{Start: start, End: end},
			mvf.fileMeta.FileSize,
			mvf.fileMeta.AesKey,
			mvf.fileMeta.AesIv,
			func(ctx context.Context, s, e int64) (io.ReadCloser, error) {
				// Create usenet reader first for encrypted data
				return mvf.createUsenetReader(ctx, s, e)
			},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create AES decrypt reader: %w", err)
		}
		return decryptedReader, nil

	default:
		return nil, fmt.Errorf("unsupported encryption type: %v", mvf.fileMeta.Encryption)
	}
}

// updateFileHealthOnError updates both metadata and database health status when corruption is detected.
// Uses synchronous operations with timeout to prevent goroutine leaks.
func (mvf *MetadataVirtualFile) updateFileHealthOnError(dataCorruptionErr *usenet.DataCorruptionError, noRetry bool) {
	// Use a short timeout context to prevent blocking indefinitely
	ctx, cancel := context.WithTimeout(mvf.ctx, 5*time.Second)
	defer cancel()

	// Any file with missing segments or corruption is marked as corrupted in metadata
	// but set to pending in DB to trigger the repair cycle immediately
	metadataStatus := metapb.FileStatus_FILE_STATUS_CORRUPTED
	dbStatus := database.HealthStatusPending

	// Update metadata status (blocking with timeout)
	if err := mvf.metadataService.UpdateFileStatus(mvf.name, metadataStatus); err != nil {
		slog.WarnContext(ctx, "Failed to update metadata status", "file", mvf.name, "error", err)
	}

	// Update database health tracking (blocking with timeout)
	errorMsg := dataCorruptionErr.Error()
	sourceNzbPath := &mvf.fileMeta.SourceNzbPath
	if *sourceNzbPath == "" {
		sourceNzbPath = nil
	}

	// Create error details JSON
	errorDetails := fmt.Sprintf(`{"missing_articles": %d, "total_articles": %d, "error_type": "ArticleNotFound"}`,
		1, len(mvf.fileMeta.SegmentData))

	// Increment streaming failure count and handle masking
	cfg := mvf.configGetter()
	if cfg.Streaming.FailureMasking.Enabled == nil || *cfg.Streaming.FailureMasking.Enabled {
		isMasked, err := mvf.healthRepository.IncrementStreamingFailureCount(ctx, mvf.name, cfg.Streaming.FailureMasking.Threshold)
		if err != nil {
			slog.WarnContext(ctx, "Failed to increment streaming failure count", "file", mvf.name, "error", err)
		} else if isMasked {
			slog.InfoContext(ctx, "File masked due to repeated streaming failures", "file", mvf.name, "threshold", cfg.Streaming.FailureMasking.Threshold)
		}
	}

	if err := mvf.healthRepository.UpdateFileHealth(
		ctx,
		mvf.name,
		dbStatus,
		&errorMsg,
		sourceNzbPath,
		&errorDetails,
		noRetry,
	); err != nil {
		slog.WarnContext(ctx, "Failed to update file health", "file", mvf.name, "error", err)
	}
}

// readFullContext reads exactly len(buf) bytes from r, but returns early
// if ctx is cancelled. This prevents io.ReadFull from blocking indefinitely
// when the underlying reader is stuck (e.g., waiting for network data).
func readFullContext(ctx context.Context, r io.Reader, buf []byte) (int, error) {
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := io.ReadFull(r, buf)
		ch <- result{n, err}
	}()
	select {
	case res := <-ch:
		return res.n, res.err
	case <-ctx.Done():
		// Context cancelled — the goroutine will finish eventually when
		// the reader is closed by the caller's defer reader.Close().
		return 0, ctx.Err()
	}
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

func (mrf *MetadataRemoteFile) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return mrf.metadataService.CreateDirectory(name)
}

func (mrf *MetadataRemoteFile) MkdirAll(ctx context.Context, name string, perm os.FileMode) error {
	return mrf.metadataService.CreateDirectory(name)
}

// resolveIDPath resolves a sharded ID path (.ids/...) to the actual virtual path
func (mrf *MetadataRemoteFile) resolveIDPath(idPath string) (string, error) {
	cfg := mrf.configGetter()
	metadataRoot := cfg.Metadata.RootPath

	// The idPath is like .ids/4/0/e/9/a/40e9a6c9-e922-4217-ab6c-9d2207528a78
	// The metadata file is at metadataRoot/.ids/4/0/e/9/a/40e9a6c9-e922-4217-ab6c-9d2207528a78.meta

	// Ensure it has .meta extension for the check
	fullIdPath := filepath.Join(metadataRoot, idPath+".meta")

	// Read the symlink
	target, err := os.Readlink(fullIdPath)
	if err != nil {
		return "", err
	}

	// The target is relative to the directory of the symlink
	// e.g. ../../../../../movies/Spider-Man.../Spider-Man....meta

	// Calculate the absolute path of the target metadata file
	absTarget := filepath.Join(filepath.Dir(fullIdPath), target)

	// Calculate the relative path from metadataRoot to get the virtual path
	relPath, err := filepath.Rel(metadataRoot, absTarget)
	if err != nil {
		return "", err
	}

	// Remove .meta extension to get the virtual filename
	virtualPath := strings.TrimSuffix(relPath, ".meta")

	return virtualPath, nil
}
