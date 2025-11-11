package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/utils"
	"github.com/javi11/altmount/pkg/rclonecli"
)

// SyncProgress tracks the progress of an ongoing library sync
type SyncProgress struct {
	TotalFiles     int       `json:"total_files"`
	ProcessedFiles int       `json:"processed_files"`
	StartTime      time.Time `json:"start_time"`
}

// SyncResult stores the results of a completed library sync
type SyncResult struct {
	FilesAdded      int           `json:"files_added"`
	FilesDeleted    int           `json:"files_deleted"`
	MetadataDeleted int           `json:"metadata_deleted"`
	Duration        time.Duration `json:"duration"`
	CompletedAt     time.Time     `json:"completed_at"`
}

// LibrarySyncStatus represents the current status of the library sync worker
type LibrarySyncStatus struct {
	IsRunning      bool          `json:"is_running"`
	Progress       *SyncProgress `json:"progress,omitempty"`
	LastSyncResult *SyncResult   `json:"last_sync_result,omitempty"`
}

// LibraryFiles holds both symlinks and STRM files found in the library directory
type LibraryFiles struct {
	SymlinkPaths []string           // List of cleaned symlink target paths pointing to mount
	StrmPaths    map[string]struct{} // Set of virtual paths (without .strm extension) that have .strm files
}

// ImportDirFiles holds both regular files and STRM files found in the import directory
type ImportDirFiles struct {
	RegularFiles map[string]struct{} // Set of virtual paths for regular files
	StrmFiles    map[string]struct{} // Set of virtual paths with .strm files (without .strm extension)
}

// LibrarySyncWorker manages automatic health check library synchronization
type LibrarySyncWorker struct {
	metadataService *metadata.MetadataService
	healthRepo      *database.HealthRepository
	configGetter    config.ConfigGetter
	cancelFunc      context.CancelFunc
	mu              sync.Mutex
	running         bool
	progressMu      sync.RWMutex
	progress        *SyncProgress
	lastSyncResult  *SyncResult
	manualTrigger   chan struct{}
	symlinkFinder   *utils.SymlinkFinder
	rcloneClient    rclonecli.RcloneRcClient
}

// NewLibrarySyncWorker creates a new library sync worker
func NewLibrarySyncWorker(
	metadataService *metadata.MetadataService,
	healthRepo *database.HealthRepository,
	configGetter config.ConfigGetter,
	rcloneClient rclonecli.RcloneRcClient,
	symlinkFinder *utils.SymlinkFinder,
) *LibrarySyncWorker {
	return &LibrarySyncWorker{
		metadataService: metadataService,
		healthRepo:      healthRepo,
		configGetter:    configGetter,
		symlinkFinder:   symlinkFinder,
		rcloneClient:    rcloneClient,
		manualTrigger:   make(chan struct{}, 1), // Buffered channel for non-blocking sends
	}
}

// StartLibrarySync starts the library sync worker in a background goroutine
func (lsw *LibrarySyncWorker) StartLibrarySync(ctx context.Context) {
	lsw.mu.Lock()
	defer lsw.mu.Unlock()

	if lsw.running {
		slog.WarnContext(ctx, "Library sync worker already running")
		return
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	lsw.cancelFunc = cancel
	lsw.running = true

	go lsw.run(ctx)
}

// Stop stops the library sync worker
func (lsw *LibrarySyncWorker) Stop(ctx context.Context) {
	lsw.mu.Lock()
	defer lsw.mu.Unlock()

	if !lsw.running {
		slog.WarnContext(ctx, "Library sync worker not running")
		return
	}

	if lsw.cancelFunc != nil {
		lsw.cancelFunc()
		lsw.cancelFunc = nil
	}
	lsw.running = false
	slog.InfoContext(ctx, "Library sync worker stopped")
}

// IsRunning returns whether the library sync worker is currently running
func (lsw *LibrarySyncWorker) IsRunning() bool {
	lsw.mu.Lock()
	defer lsw.mu.Unlock()
	return lsw.running
}

// GetStatus returns the current status of the library sync worker
func (lsw *LibrarySyncWorker) GetStatus() LibrarySyncStatus {
	lsw.progressMu.RLock()
	defer lsw.progressMu.RUnlock()

	status := LibrarySyncStatus{
		IsRunning: lsw.progress != nil,
	}

	// Copy progress if available
	if lsw.progress != nil {
		progressCopy := *lsw.progress
		status.Progress = &progressCopy
	}

	// Copy last sync result if available
	if lsw.lastSyncResult != nil {
		resultCopy := *lsw.lastSyncResult
		status.LastSyncResult = &resultCopy
	}

	return status
}

// TriggerManualSync triggers a manual library sync
func (lsw *LibrarySyncWorker) TriggerManualSync(ctx context.Context) error {
	lsw.mu.Lock()
	running := lsw.running
	lsw.mu.Unlock()

	if !running {
		return fmt.Errorf("library sync worker is not running")
	}

	// Non-blocking send to trigger channel
	select {
	case lsw.manualTrigger <- struct{}{}:
		slog.InfoContext(ctx, "Manual library sync triggered")
		return nil
	default:
		// Channel already has a pending trigger
		return fmt.Errorf("library sync already triggered or in progress")
	}
}

// run is the main library sync loop
func (lsw *LibrarySyncWorker) run(ctx context.Context) {
	defer func() {
		lsw.mu.Lock()
		lsw.running = false
		lsw.mu.Unlock()
	}()

	cfg := lsw.configGetter()

	// Only run if health system is enabled
	if cfg.Health.Enabled == nil || !*cfg.Health.Enabled {
		slog.InfoContext(ctx, "Library sync disabled (health system is disabled)")
		return
	}

	if cfg.Health.LibrarySyncIntervalMinutes <= 0 {
		slog.InfoContext(ctx, "Library sync disabled (interval is 0)")
		return
	}

	interval := time.Duration(cfg.Health.LibrarySyncIntervalMinutes) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.InfoContext(ctx, "Library sync worker started",
		"interval_minutes", cfg.Health.LibrarySyncIntervalMinutes)

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "Library sync worker stopped by context")
			return
		case <-ticker.C:
			lsw.syncLibrary(ctx)
		case <-lsw.manualTrigger:
			slog.InfoContext(ctx, "Manual library sync trigger received")
			lsw.syncLibrary(ctx)
		}
	}
}

// syncLibrary performs a full library synchronization
func (lsw *LibrarySyncWorker) syncLibrary(ctx context.Context) {
	startTime := time.Now()
	cfg := lsw.configGetter()
	slog.InfoContext(ctx, "Starting library sync")

	// Initialize progress tracking
	lsw.progressMu.Lock()
	lsw.progress = &SyncProgress{
		TotalFiles:     0, // Will be updated once we know the total
		ProcessedFiles: 0,
		StartTime:      startTime,
	}
	lsw.progressMu.Unlock()

	// Clear progress and set result when done
	defer func() {
		lsw.progressMu.Lock()
		lsw.progress = nil
		lsw.progressMu.Unlock()
	}()

	// Get all metadata files from filesystem
	metadataFiles, err := lsw.getAllMetadataFiles(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get metadata files", "error", err)
		return
	}

	// Update total files count
	lsw.progressMu.Lock()
	lsw.progress.TotalFiles = len(metadataFiles)
	lsw.progressMu.Unlock()

	// Get all health check paths from database
	dbPaths, err := lsw.healthRepo.GetAllHealthCheckPaths(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get automatic health check paths from database", "error", err)
		return
	}

	// Convert to maps for efficient lookup
	metaFileSet := make(map[string]struct{}, len(metadataFiles))
	for _, path := range metadataFiles {
		virtualPath := lsw.metaPathToVirtualPath(path)
		metaFileSet[virtualPath] = struct{}{}
	}

	dbPathSet := make(map[string]struct{}, len(dbPaths))
	for _, path := range dbPaths {
		dbPathSet[path] = struct{}{}
	}

	// Find files to add (in filesystem but not in database)
	var filesToAdd []database.AutomaticHealthCheckRecord
	for i, metaPath := range metadataFiles {
		select {
		case <-ctx.Done():
			return
		default:
		}

		virtualPath := lsw.metaPathToVirtualPath(metaPath)
		if _, exists := dbPathSet[virtualPath]; !exists {
			// Read metadata to get release date
			fileMeta, err := lsw.metadataService.ReadFileMetadata(virtualPath)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to read metadata",
					"virtual_path", virtualPath,
					"error", err)
				continue
			}
			if fileMeta == nil {
				continue
			}

			// Convert Unix timestamp to time.Time
			releaseDate := time.Unix(fileMeta.ReleaseDate, 0)

			// Calculate initial check time
			scheduledCheckAt := calculateInitialCheck(releaseDate)

			filesToAdd = append(filesToAdd, database.AutomaticHealthCheckRecord{
				FilePath:         virtualPath,
				ReleaseDate:      releaseDate,
				ScheduledCheckAt: scheduledCheckAt,
				SourceNzbPath:    &fileMeta.SourceNzbPath,
			})
		}

		// Update progress every 100 files to avoid excessive locking
		if i%100 == 0 || i == len(metadataFiles)-1 {
			lsw.progressMu.Lock()
			if lsw.progress != nil {
				lsw.progress.ProcessedFiles = i + 1
			}
			lsw.progressMu.Unlock()
		}
	}

	// Find files to delete (in database but not in filesystem)
	var filesToDelete []string
	for _, dbPath := range dbPaths {
		if _, exists := metaFileSet[dbPath]; !exists {
			filesToDelete = append(filesToDelete, dbPath)
		}
	}

	// Perform batch operations
	addedCount := 0
	deletedCount := 0

	if len(filesToAdd) > 0 {
		if err := lsw.healthRepo.BatchAddAutomaticHealthChecks(ctx, filesToAdd); err != nil {
			slog.ErrorContext(ctx, "Failed to batch add automatic health checks",
				"count", len(filesToAdd),
				"error", err)
		} else {
			addedCount = len(filesToAdd)
			slog.InfoContext(ctx, "Added new files to automatic health checks",
				"count", addedCount)
		}
	}

	if len(filesToDelete) > 0 {
		if err := lsw.healthRepo.DeleteHealthRecordsBulk(ctx, filesToDelete); err != nil {
			slog.ErrorContext(ctx, "Failed to delete orphaned health records",
				"count", len(filesToDelete),
				"error", err)
		} else {
			deletedCount = len(filesToDelete)
			slog.InfoContext(ctx, "Deleted orphaned health records",
				"count", deletedCount)
		}
	}

	// Additional cleanup of orphaned metadata files if enabled
	metadataDeletedCount := 0
	if cfg.Health.CleanupOrphanedMetadata != nil && *cfg.Health.CleanupOrphanedMetadata {
		// Get all library files (symlinks and .strm) in one pass
		libraryFiles, err := lsw.getAllLibraryFiles(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.ErrorContext(ctx, "Failed to get library files", "error", err)
			}
			return
		}

		// Get all import_dir files (regular and .strm) in one pass
		importDirFiles, err := lsw.getAllImportDirFiles(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.ErrorContext(ctx, "Failed to get import directory files", "error", err)
			}
			return
		}

		// Only validate symlinks if SYMLINK strategy is enabled
		if cfg.Import.ImportStrategy == config.ImportStrategySYMLINK {
			metadataDeletedCount, err = lsw.validateSymlinks(ctx, metadataFiles, libraryFiles.SymlinkPaths, importDirFiles)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}

				slog.ErrorContext(ctx, "Failed to validate symlinks", "error", err)
				return
			}
		}

		// Also validate STRM files if STRM is enabled
		if cfg.Import.ImportStrategy == config.ImportStrategySTRM {
			strmDeletedCount, err := lsw.validateStrmFiles(ctx, metadataFiles, libraryFiles.StrmPaths, importDirFiles)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}

				slog.ErrorContext(ctx, "Failed to validate STRM files", "error", err)
				// Don't return here - log error but continue
			} else if strmDeletedCount > 0 {
				slog.InfoContext(ctx, "Deleted metadata without STRM files",
					"count", strmDeletedCount)
				metadataDeletedCount += strmDeletedCount
			}
		}
	}

	duration := time.Since(startTime)

	// Store sync result
	lsw.progressMu.Lock()
	lsw.lastSyncResult = &SyncResult{
		FilesAdded:      addedCount,
		FilesDeleted:    deletedCount,
		MetadataDeleted: metadataDeletedCount,
		Duration:        duration,
		CompletedAt:     time.Now(),
	}
	lsw.progressMu.Unlock()

	slog.InfoContext(ctx, "Library sync completed",
		"total_metadata_files", len(metadataFiles),
		"total_db_records", len(dbPaths),
		"added", addedCount,
		"deleted", deletedCount,
		"metadata_deleted", metadataDeletedCount,
		"duration", duration)
}

// getAllMetadataFiles collects all .meta files from the filesystem
func (lsw *LibrarySyncWorker) getAllMetadataFiles(ctx context.Context) ([]string, error) {
	cfg := lsw.configGetter()
	rootPath := cfg.Metadata.RootPath

	var metaFiles []string
	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // Skip errors
		}

		// Only include .meta files
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".meta") {
			metaFiles = append(metaFiles, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return metaFiles, nil
}

// metaPathToVirtualPath converts a metadata file path to a virtual file path
func (lsw *LibrarySyncWorker) metaPathToVirtualPath(metaPath string) string {
	cfg := lsw.configGetter()
	rootPath := cfg.Metadata.RootPath

	// Remove root path and .meta extension
	relativePath := strings.TrimPrefix(metaPath, rootPath)
	relativePath = strings.TrimPrefix(relativePath, string(filepath.Separator))
	virtualPath := strings.TrimSuffix(relativePath, ".meta")

	return virtualPath
}

// validateSymlinks validates metadata files against library symlinks and deletes orphaned metadata
func (lsw *LibrarySyncWorker) validateSymlinks(ctx context.Context, metadataFiles []string, symlinkPaths []string, importDirFiles *ImportDirFiles) (int, error) {
	slog.InfoContext(ctx, "Starting symlink validation for library directory")

	// Build map of virtual paths that have symlinks
	symlinkSet := make(map[string]struct{}, len(symlinkPaths))
	for _, path := range symlinkPaths {
		symlinkSet[path] = struct{}{}
	}

	deletedCount := 0

	// Check each metadata file
	for _, metaPath := range metadataFiles {
		select {
		case <-ctx.Done():
			return deletedCount, ctx.Err()
		default:
		}

		virtualPath := lsw.metaPathToVirtualPath(metaPath)
		mountPath := filepath.Join(lsw.configGetter().MountPath, virtualPath)

		// Check if the file in mount path is actually a symlink
		// Use Lstat to not follow symlinks
		fileInfo, err := os.Lstat(mountPath)
		if err != nil {
			// File doesn't exist in mount - skip (might be managed differently)
			continue
		}

		// Skip if not a symlink - this file is not managed as a symlink
		if fileInfo.Mode()&os.ModeSymlink == 0 {
			continue
		}

		// Check if this symlink exists in the library directory
		target := filepath.Clean(mountPath)
		_, inLibrary := symlinkSet[target]

		// Check if file exists in import_dir (as regular or .strm)
		inImportDir := false
		if importDirFiles != nil {
			_, hasRegular := importDirFiles.RegularFiles[virtualPath]
			_, hasStrm := importDirFiles.StrmFiles[virtualPath]
			inImportDir = hasRegular || hasStrm
		}

		// Only delete if missing from BOTH library and import_dir
		if !inLibrary && !inImportDir {
			slog.InfoContext(ctx, "Deleting metadata without library symlink",
				"virtual_path", virtualPath,
				"mount_path", target)

			// Delete from database
			if err := lsw.healthRepo.DeleteHealthRecordsBulk(ctx, []string{virtualPath}); err != nil {
				slog.ErrorContext(ctx, "Failed to delete health record",
					"virtual_path", virtualPath,
					"error", err)
			}

			// Delete from metadata
			if err := lsw.metadataService.DeleteFileMetadata(virtualPath); err != nil {
				slog.ErrorContext(ctx, "Failed to delete metadata",
					"virtual_path", virtualPath,
					"error", err)
			}

			// Refresh mount cache for the directory
			dirPath := filepath.Dir(virtualPath)
			if err := lsw.rcloneClient.RefreshDir(ctx, config.MountProvider, []string{dirPath}); err != nil {
				slog.WarnContext(ctx, "Failed to refresh mount cache",
					"dir_path", dirPath,
					"error", err)
			}

			deletedCount++
		}
	}

	slog.InfoContext(ctx, "Symlink validation completed",
		"metadata_deleted", deletedCount,
		"symlinks_found", len(symlinkPaths))

	return deletedCount, nil
}

// getAllLibraryFiles collects both symlinks and .strm files from library directory in a single pass
func (lsw *LibrarySyncWorker) getAllLibraryFiles(ctx context.Context) (*LibraryFiles, error) {
	cfg := lsw.configGetter()

	// Get library directory
	if cfg.Health.LibraryDir == nil || *cfg.Health.LibraryDir == "" {
		return nil, fmt.Errorf("library directory is not configured")
	}

	libraryDir := *cfg.Health.LibraryDir
	mountDir := cfg.MountPath

	result := &LibraryFiles{
		SymlinkPaths: []string{},
		StrmPaths:    make(map[string]struct{}),
	}

	// Walk the library directory recursively once
	err := filepath.WalkDir(libraryDir, func(path string, d os.DirEntry, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // Continue walking despite errors
		}

		// Check if it's a symlink
		if d.Type()&os.ModeSymlink != 0 {
			// Read the symlink target
			target, err := os.Readlink(path)
			if err != nil {
				slog.WarnContext(ctx, "Failed to read symlink", "path", path, "error", err)
				return nil
			}

			// Make target absolute if it's relative
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}

			// Clean the paths for comparison
			cleanTarget := filepath.Clean(target)
			cleanMountDir := filepath.Clean(mountDir)

			// Check if this symlink points inside the mount directory
			if strings.HasPrefix(cleanTarget, cleanMountDir) {
				result.SymlinkPaths = append(result.SymlinkPaths, cleanTarget)
			}
		}

		// Check if it's a .strm file
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".strm") {
			// Get relative path from library_dir
			relativePath, err := filepath.Rel(libraryDir, path)
			if err != nil {
				slog.WarnContext(ctx, "Failed to get relative path for STRM file",
					"path", path,
					"error", err)
				return nil
			}

			// Remove .strm extension to get virtual path
			virtualPath := strings.TrimSuffix(relativePath, ".strm")

			// Normalize path separators
			virtualPath = filepath.ToSlash(virtualPath)

			result.StrmPaths[virtualPath] = struct{}{}
		}

		return nil
	})

	if err != nil {
		slog.ErrorContext(ctx, "Error during library file scan", "error", err)
		return nil, err
	}

	return result, nil
}

// getAllImportDirFiles collects both regular files and .strm files from import directory in a single pass
func (lsw *LibrarySyncWorker) getAllImportDirFiles(ctx context.Context) (*ImportDirFiles, error) {
	cfg := lsw.configGetter()

	// Get import directory
	if cfg.Import.ImportDir == nil || *cfg.Import.ImportDir == "" {
		// No import directory configured - return empty result
		return &ImportDirFiles{
			RegularFiles: make(map[string]struct{}),
			StrmFiles:    make(map[string]struct{}),
		}, nil
	}

	importDir := *cfg.Import.ImportDir

	// Check if directory exists
	if _, err := os.Stat(importDir); os.IsNotExist(err) {
		slog.WarnContext(ctx, "Import directory does not exist", "import_dir", importDir)
		return &ImportDirFiles{
			RegularFiles: make(map[string]struct{}),
			StrmFiles:    make(map[string]struct{}),
		}, nil
	}

	result := &ImportDirFiles{
		RegularFiles: make(map[string]struct{}),
		StrmFiles:    make(map[string]struct{}),
	}

	// Walk the import directory recursively once
	err := filepath.WalkDir(importDir, func(path string, d os.DirEntry, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // Continue walking despite errors
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Get relative path from import_dir
		relativePath, err := filepath.Rel(importDir, path)
		if err != nil {
			slog.WarnContext(ctx, "Failed to get relative path for import file",
				"path", path,
				"error", err)
			return nil
		}

		// Normalize path separators
		virtualPath := filepath.ToSlash(relativePath)

		// Check if it's a .strm file
		if strings.HasSuffix(d.Name(), ".strm") {
			// STRM file - add without .strm extension
			result.StrmFiles[strings.TrimSuffix(virtualPath, ".strm")] = struct{}{}
		} else {
			// Regular file
			result.RegularFiles[virtualPath] = struct{}{}
		}

		return nil
	})

	if err != nil {
		slog.ErrorContext(ctx, "Error during import directory file scan", "error", err)
		return nil, err
	}

	return result, nil
}

// virtualPathToStrmPath converts a virtual path to its corresponding STRM path
// Example: "Movies/Example.mkv" -> "Movies/Example.mkv.strm"
func virtualPathToStrmPath(virtualPath string) string {
	// Keep original filename and add .strm extension (matches creation logic)
	return virtualPath + ".strm"
}

// validateStrmFiles validates metadata files against STRM files and deletes orphaned metadata
func (lsw *LibrarySyncWorker) validateStrmFiles(ctx context.Context, metadataFiles []string, strmPaths map[string]struct{}, importDirFiles *ImportDirFiles) (int, error) {
	slog.InfoContext(ctx, "Starting STRM file validation")

	deletedCount := 0

	// Check each metadata file
	for _, metaPath := range metadataFiles {
		select {
		case <-ctx.Done():
			return deletedCount, ctx.Err()
		default:
		}

		virtualPath := lsw.metaPathToVirtualPath(metaPath)

		// Convert virtual path to expected STRM path
		strmVirtualPath := virtualPathToStrmPath(virtualPath)

		// Check if this file should be managed as a STRM file
		// A file is managed as STRM if a .strm file exists (or should exist) for it
		cfg := lsw.configGetter()
		if cfg.Import.ImportDir == nil || *cfg.Import.ImportDir == "" {
			// No STRM directory configured - skip STRM validation
			continue
		}

		strmFilePath := filepath.Join(*cfg.Import.ImportDir, strmVirtualPath)

		// Check if .strm file exists
		if _, err := os.Stat(strmFilePath); os.IsNotExist(err) {
			// .strm file doesn't exist - check if this file is in the STRM path set
			// If it's not in the set, it means it's not managed as a STRM file - skip it
			if _, inStrmSet := strmPaths[strmVirtualPath]; !inStrmSet {
				// This file is not managed as a STRM file - skip
				continue
			}
		}

		// Check if STRM file exists in library
		_, inLibrary := strmPaths[strmVirtualPath]

		// Check if file exists in import_dir (as regular or .strm)
		inImportDir := false
		if importDirFiles != nil {
			_, hasRegular := importDirFiles.RegularFiles[virtualPath]
			_, hasStrm := importDirFiles.StrmFiles[virtualPath]
			inImportDir = hasRegular || hasStrm
		}

		// Only delete if missing from BOTH library and import_dir
		if !inLibrary && !inImportDir {
			slog.InfoContext(ctx, "Deleting metadata without STRM file",
				"virtual_path", virtualPath,
				"expected_strm_path", strmVirtualPath)

			// Delete from database
			if err := lsw.healthRepo.DeleteHealthRecordsBulk(ctx, []string{virtualPath}); err != nil {
				slog.ErrorContext(ctx, "Failed to delete health record",
					"virtual_path", virtualPath,
					"error", err)
			}

			// Delete from metadata
			if err := lsw.metadataService.DeleteFileMetadata(virtualPath); err != nil {
				slog.ErrorContext(ctx, "Failed to delete metadata",
					"virtual_path", virtualPath,
					"error", err)
			}

			// Refresh mount cache for the directory
			dirPath := filepath.Dir(virtualPath)
			if err := lsw.rcloneClient.RefreshDir(ctx, config.MountProvider, []string{dirPath}); err != nil {
				slog.WarnContext(ctx, "Failed to refresh mount cache",
					"dir_path", dirPath,
					"error", err)
			}

			deletedCount++
		}
	}

	slog.InfoContext(ctx, "STRM file validation completed",
		"metadata_deleted", deletedCount,
		"strm_files_found", len(strmPaths))

	return deletedCount, nil
}
