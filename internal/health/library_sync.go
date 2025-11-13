package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/pkg/rclonecli"
	"github.com/sourcegraph/conc/pool"
)

// SyncProgress tracks the progress of an ongoing library sync
type SyncProgress struct {
	TotalFiles     int          `json:"total_files"`
	ProcessedFiles atomic.Int32 `json:"processed_files"`
	StartTime      time.Time    `json:"start_time"`
}

// SyncResult stores the results of a completed library sync
type SyncResult struct {
	FilesAdded          int           `json:"files_added"`
	FilesDeleted        int           `json:"files_deleted"`
	MetadataDeleted     int           `json:"metadata_deleted"`
	LibraryFilesDeleted int           `json:"library_files_deleted"`
	LibraryDirsDeleted  int           `json:"library_dirs_deleted"`
	Duration            time.Duration `json:"duration"`
	CompletedAt         time.Time     `json:"completed_at"`
}

// LibrarySyncStatus represents the current status of the library sync worker
type LibrarySyncStatus struct {
	IsRunning      bool          `json:"is_running"`
	Progress       *SyncProgress `json:"progress,omitempty"`
	LastSyncResult *SyncResult   `json:"last_sync_result,omitempty"`
}

// LibraryFiles holds both symlinks and STRM files found in the library directory
type UsedFiles struct {
	Symlinks  map[string]string // Map of mount target path -> library symlink path
	StrmFiles map[string]string // Map of virtual path (without .strm) -> library .strm file path
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
	rcloneClient    rclonecli.RcloneRcClient
}

// NewLibrarySyncWorker creates a new library sync worker
func NewLibrarySyncWorker(
	metadataService *metadata.MetadataService,
	healthRepo *database.HealthRepository,
	configGetter config.ConfigGetter,
	rcloneClient rclonecli.RcloneRcClient,
) *LibrarySyncWorker {
	return &LibrarySyncWorker{
		metadataService: metadataService,
		healthRepo:      healthRepo,
		configGetter:    configGetter,
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
		processedFiles := lsw.progress.ProcessedFiles.Load()
		progressCopy := SyncProgress{
			TotalFiles:     lsw.progress.TotalFiles,
			ProcessedFiles: atomic.Int32{},
			StartTime:      lsw.progress.StartTime,
		}
		progressCopy.ProcessedFiles.Store(processedFiles)
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

// syncMaps holds the metadata and database record maps used during synchronization
type syncMaps struct {
	metaFileSet map[string]string                                  // mount relative path -> metadata file path
	dbPathSet   map[string]database.AutomaticHealthCheckRecord // mount relative path -> health check record
}

// syncCounts holds the results of database synchronization operations
type syncCounts struct {
	added   int
	deleted int
}

// cleanupCounts holds the results of cleanup operations
type cleanupCounts struct {
	metadataDeleted     int
	libraryFilesDeleted int
	libraryDirsDeleted  int
}

// findFilesToDelete identifies database records that no longer have corresponding metadata files
// or library files. filesInUse can be nil for metadata-only sync.
func (lsw *LibrarySyncWorker) findFilesToDelete(
	ctx context.Context,
	dbRecords []database.AutomaticHealthCheckRecord,
	metaFileSet map[string]string,
	filesInUse map[string]string,
) []string {
	var filesToDelete []string

	for _, dbRecord := range dbRecords {
		select {
		case <-ctx.Done():
			return filesToDelete
		default:
		}

		// Check if metadata file exists
		if _, exists := metaFileSet[dbRecord.FilePath]; !exists {
			filesToDelete = append(filesToDelete, dbRecord.FilePath)
			continue
		}

		// Check if file is in use (only for full sync, not metadata-only)
		if filesInUse != nil {
			if _, exists := filesInUse[dbRecord.FilePath]; !exists {
				filesToDelete = append(filesToDelete, dbRecord.FilePath)
			}
		}
	}

	return filesToDelete
}

// recordSyncResult stores the sync result and logs completion information
func (lsw *LibrarySyncWorker) recordSyncResult(
	ctx context.Context,
	startTime time.Time,
	dbCounts syncCounts,
	cleanup cleanupCounts,
	totalMetadataFiles int,
	totalDbRecords int,
) {
	duration := time.Since(startTime)

	// Store sync result
	lsw.progressMu.Lock()
	lsw.lastSyncResult = &SyncResult{
		FilesAdded:          dbCounts.added,
		FilesDeleted:        dbCounts.deleted,
		MetadataDeleted:     cleanup.metadataDeleted,
		LibraryFilesDeleted: cleanup.libraryFilesDeleted,
		LibraryDirsDeleted:  cleanup.libraryDirsDeleted,
		Duration:            duration,
		CompletedAt:         time.Now(),
	}
	lsw.progressMu.Unlock()

	// Log completion
	slog.InfoContext(ctx, "Library sync completed",
		"total_metadata_files", totalMetadataFiles,
		"total_db_records", totalDbRecords,
		"added", dbCounts.added,
		"deleted", dbCounts.deleted,
		"metadata_deleted", cleanup.metadataDeleted,
		"library_files_deleted", cleanup.libraryFilesDeleted,
		"library_dirs_deleted", cleanup.libraryDirsDeleted,
		"duration", duration)
}

// syncDatabaseRecords performs batch add and delete operations on the health check database
// Returns the number of records added and deleted
func (lsw *LibrarySyncWorker) syncDatabaseRecords(
	ctx context.Context,
	filesToAdd []database.AutomaticHealthCheckRecord,
	filesToDelete []string,
) syncCounts {
	counts := syncCounts{}

	// Batch add new files
	if len(filesToAdd) > 0 {
		if err := lsw.healthRepo.BatchAddAutomaticHealthChecks(ctx, filesToAdd); err != nil {
			slog.ErrorContext(ctx, "Failed to batch add automatic health checks",
				"count", len(filesToAdd),
				"error", err)
		} else {
			counts.added = len(filesToAdd)
			slog.InfoContext(ctx, "Added new files to automatic health checks",
				"count", counts.added)
		}
	}

	// Batch delete orphaned files
	if len(filesToDelete) > 0 {
		if err := lsw.healthRepo.DeleteHealthRecordsBulk(ctx, filesToDelete); err != nil {
			slog.ErrorContext(ctx, "Failed to delete orphaned health records",
				"count", len(filesToDelete),
				"error", err)
		} else {
			counts.deleted = len(filesToDelete)
			slog.InfoContext(ctx, "Deleted orphaned health records",
				"count", counts.deleted)
		}
	}

	return counts
}

// buildSyncMaps constructs the lookup maps for metadata files and database records
func (lsw *LibrarySyncWorker) buildSyncMaps(metadataFiles []string, dbRecords []database.AutomaticHealthCheckRecord) syncMaps {
	// Convert metadata files to map for efficient lookup
	metaFileSet := make(map[string]string, len(metadataFiles))
	for _, path := range metadataFiles {
		mountRelativePath := lsw.metaPathToMountRelativePath(path)
		metaFileSet[mountRelativePath] = path
	}

	// Convert database records to map for efficient lookup
	dbPathSet := make(map[string]database.AutomaticHealthCheckRecord, len(dbRecords))
	for _, record := range dbRecords {
		// Use mount relative path as the key
		dbPathSet[record.FilePath] = record
	}

	return syncMaps{
		metaFileSet: metaFileSet,
		dbPathSet:   dbPathSet,
	}
}

// initializeProgressTracking initializes progress tracking for a sync operation
// and returns a cleanup function to be called when sync completes
func (lsw *LibrarySyncWorker) initializeProgressTracking(startTime time.Time) func() {
	lsw.progressMu.Lock()
	lsw.progress = &SyncProgress{
		TotalFiles:     0, // Will be updated once we know the total
		ProcessedFiles: atomic.Int32{},
		StartTime:      startTime,
	}
	lsw.progressMu.Unlock()

	// Return cleanup function
	return func() {
		lsw.progressMu.Lock()
		lsw.progress = nil
		lsw.progressMu.Unlock()
	}
}

// syncLibrary performs a full library synchronization
func (lsw *LibrarySyncWorker) syncLibrary(ctx context.Context) {
	startTime := time.Now()
	cfg := lsw.configGetter()
	slog.InfoContext(ctx, "Starting library sync")

	// Check import strategy - if NONE, only sync DB with metadata files
	if cfg.Import.ImportStrategy == config.ImportStrategyNone {
		slog.InfoContext(ctx, "Import strategy is NONE, performing metadata-only sync")
		lsw.syncMetadataOnly(ctx, startTime)
		return
	}

	// Initialize progress tracking
	defer lsw.initializeProgressTracking(startTime)()

	// Parallelize filesystem walks for better performance
	var metadataFiles []string
	var libraryFiles *UsedFiles
	var importDirFiles *UsedFiles

	fsWalkPool := pool.New().WithErrors().WithMaxGoroutines(3)

	// Get all metadata files from filesystem
	fsWalkPool.Go(func() error {
		files, err := lsw.getAllMetadataFiles(ctx)
		if err != nil {
			return fmt.Errorf("failed to get metadata files: %w", err)
		}
		metadataFiles = files
		return nil
	})

	// Get all library files (symlinks and .strm) to capture library paths
	fsWalkPool.Go(func() error {
		files, err := lsw.getAllLibraryFiles(ctx)
		if err != nil {
			return fmt.Errorf("failed to get library files: %w", err)
		}
		libraryFiles = files
		return nil
	})

	// Get all import directory files
	fsWalkPool.Go(func() error {
		files, err := lsw.getAllImportDirFiles(ctx)
		if err != nil {
			return fmt.Errorf("failed to get import directory files: %w", err)
		}
		importDirFiles = files
		return nil
	})

	if err := fsWalkPool.Wait(); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.ErrorContext(ctx, "Failed to walk filesystem", "error", err)
		}
		return
	}

	// Update total files count
	lsw.progressMu.Lock()
	lsw.progress.TotalFiles = len(metadataFiles)
	lsw.progressMu.Unlock()

	// Build a reverse map: mount path -> library path for quick lookup
	filesInUse := make(map[string]string)

	maps.Copy(filesInUse, libraryFiles.Symlinks)
	maps.Copy(filesInUse, libraryFiles.StrmFiles)
	maps.Copy(filesInUse, importDirFiles.Symlinks)
	maps.Copy(filesInUse, importDirFiles.StrmFiles)

	// Get all health check paths from database
	dbRecords, err := lsw.healthRepo.GetAllHealthCheckRecords(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get automatic health check paths from database", "error", err)
		return
	}

	// Build lookup maps for efficient searching
	syncMaps := lsw.buildSyncMaps(metadataFiles, dbRecords)
	metaFileSet := syncMaps.metaFileSet
	dbPathSet := syncMaps.dbPathSet

	// Find files to add (in filesystem but not in database)
	var filesToAdd []database.AutomaticHealthCheckRecord
	var filesToAddMu sync.Mutex

	// Get concurrency setting (default to 10 if not set)
	concurrency := cfg.Health.LibrarySyncConcurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	// Create a worker pool for parallel metadata reading
	p := pool.New().WithMaxGoroutines(concurrency)

	for mountRelativePath := range metaFileSet {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Capture loop variable for goroutine
		path := mountRelativePath

		p.Go(func() {
			// Check if needs to be added
			if it, exists := dbPathSet[path]; !exists || it.LibraryPath == nil {
				// Read metadata to get release date
				fileMeta, err := lsw.metadataService.ReadFileMetadata(path)
				if err != nil {
					slog.ErrorContext(ctx, "Failed to read metadata",
						"mount_relative_path", path,
						"error", err)
					return
				}

				if fileMeta == nil {
					return
				}

				// Use CreatedAt if ReleaseDate is missing
				releaseDate := fileMeta.ReleaseDate
				if releaseDate == 0 {
					releaseDate = fileMeta.CreatedAt
					// Update metadata file with the CreatedAt as release date
					fileMeta.ReleaseDate = releaseDate
					if err := lsw.metadataService.WriteFileMetadata(path, fileMeta); err != nil {
						slog.ErrorContext(ctx, "Failed to update metadata with release date",
							"path", path,
							"error", err)
					} else {
						slog.InfoContext(ctx, "Set release date from CreatedAt",
							"path", path,
							"release_date", time.Unix(releaseDate, 0))
					}
				}

				// Convert Unix timestamp to time.Time
				releaseDateAsTime := time.Unix(releaseDate, 0)

				// Calculate initial check time
				scheduledCheckAt := calculateInitialCheck(releaseDateAsTime)

				// Look up library path from our map
				var libraryPath *string
				mountPath := filepath.Join(cfg.MountPath, path)
				if libPath, ok := filesInUse[mountPath]; ok {
					libraryPath = &libPath
				} else if libPath, ok := filesInUse[path]; ok {
					// Try with virtual path (for STRM files)
					libraryPath = &libPath
				}

				// Protect shared slice with mutex
				filesToAddMu.Lock()
				filesToAdd = append(filesToAdd, database.AutomaticHealthCheckRecord{
					FilePath:         path,
					LibraryPath:      libraryPath,
					ReleaseDate:      releaseDateAsTime,
					ScheduledCheckAt: scheduledCheckAt,
					SourceNzbPath:    &fileMeta.SourceNzbPath,
				})
				filesToAddMu.Unlock()
			}

			if lsw.progress != nil {
				lsw.progress.ProcessedFiles.Add(1)
			}
		})
	}

	// Wait for all workers to complete
	p.Wait()

	// Additional cleanup of orphaned metadata files if enabled
	metadataDeletedCount := 0
	if cfg.Health.CleanupOrphanedMetadata != nil && *cfg.Health.CleanupOrphanedMetadata {
		// We already have libraryFiles from earlier in the function
		for relativeMountPath := range metaFileSet {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if _, exists := filesInUse[relativeMountPath]; !exists {
				err := lsw.metadataService.DeleteFileMetadata(relativeMountPath)
				if err != nil {
					slog.ErrorContext(ctx, "Failed to delete metadata file", "error", err)
				} else {
					metadataDeletedCount++
				}
			}
		}

	}

	// Cleanup orphaned library files (symlinks and STRM files without metadata)
	libraryFilesDeletedCount := 0
	libraryDirsDeletedCount := 0

	for metaPath, file := range filesInUse {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if _, exists := metaFileSet[metaPath]; !exists {
			err := os.Remove(file)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to delete metadata file", "error", err)
			} else {
				libraryFilesDeletedCount++
			}
		}
	}

	// Remove empty directories after file cleanup
	libraryDirsDeletedCount, err = lsw.removeEmptyDirectories(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.ErrorContext(ctx, "Failed to remove empty directories", "error", err)
		}
	}

	// Find files to delete (in database but not in filesystem or not in use)
	filesToDelete := lsw.findFilesToDelete(ctx, dbRecords, metaFileSet, filesInUse)

	// Perform batch operations
	dbCounts := lsw.syncDatabaseRecords(ctx, filesToAdd, filesToDelete)

	// Record sync results
	cleanup := cleanupCounts{
		metadataDeleted:     metadataDeletedCount,
		libraryFilesDeleted: libraryFilesDeletedCount,
		libraryDirsDeleted:  libraryDirsDeletedCount,
	}
	lsw.recordSyncResult(ctx, startTime, dbCounts, cleanup, len(metadataFiles), len(dbRecords))
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

// metaPathToMountRelativePath converts a metadata file path to a mount relative file path
func (lsw *LibrarySyncWorker) metaPathToMountRelativePath(metaPath string) string {
	cfg := lsw.configGetter()
	rootPath := cfg.Metadata.RootPath

	// Remove root path and .meta extension
	relativePath := strings.TrimPrefix(metaPath, rootPath)
	relativePath = strings.TrimPrefix(relativePath, string(filepath.Separator))
	mountRelativePath := strings.TrimSuffix(relativePath, ".meta")

	return mountRelativePath
}

// getAllLibraryFiles collects both symlinks and .strm files from library directory in a single pass
func (lsw *LibrarySyncWorker) getAllLibraryFiles(ctx context.Context) (*UsedFiles, error) {
	cfg := lsw.configGetter()

	// Get library directory
	if cfg.Health.LibraryDir == nil || *cfg.Health.LibraryDir == "" {
		return nil, fmt.Errorf("library directory is not configured")
	}

	libraryDir := *cfg.Health.LibraryDir
	mountDir := cfg.MountPath

	result := &UsedFiles{
		Symlinks:  make(map[string]string),
		StrmFiles: make(map[string]string),
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
				// Store mapping of mount target path -> library symlink path
				result.Symlinks[cleanTarget] = path
			}
		}

		// Check if it's a .strm file
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".strm") {
			// Read the STRM file content to extract the URL
			content, err := os.ReadFile(path)
			if err != nil {
				slog.WarnContext(ctx, "Failed to read STRM file",
					"path", path,
					"error", err)
				return nil
			}

			// Parse the URL from the file content (trim whitespace)
			urlStr := strings.TrimSpace(string(content))
			parsedURL, err := url.Parse(urlStr)
			if err != nil {
				slog.WarnContext(ctx, "Failed to parse URL from STRM file",
					"path", path,
					"url", urlStr,
					"error", err)
				return nil
			}

			// Extract the 'path' query parameter
			virtualPath := parsedURL.Query().Get("path")
			if virtualPath == "" {
				slog.WarnContext(ctx, "STRM file URL missing 'path' query parameter",
					"path", path,
					"url", urlStr)
				return nil
			}

			// Normalize path separators
			virtualPath = filepath.ToSlash(virtualPath)

			// Store mapping of virtual path -> library .strm file path
			result.StrmFiles[virtualPath] = path
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
func (lsw *LibrarySyncWorker) getAllImportDirFiles(ctx context.Context) (*UsedFiles, error) {
	cfg := lsw.configGetter()

	// Get import directory
	if cfg.Import.ImportDir == nil || *cfg.Import.ImportDir == "" {
		// No import directory configured - return empty result
		return &UsedFiles{
			Symlinks:  make(map[string]string),
			StrmFiles: make(map[string]string),
		}, nil
	}

	importDir := *cfg.Import.ImportDir

	// Check if directory exists
	if _, err := os.Stat(importDir); os.IsNotExist(err) {
		slog.WarnContext(ctx, "Import directory does not exist", "import_dir", importDir)
		return &UsedFiles{
			Symlinks:  make(map[string]string),
			StrmFiles: make(map[string]string),
		}, nil
	}

	result := &UsedFiles{
		Symlinks:  make(map[string]string),
		StrmFiles: make(map[string]string),
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
			result.StrmFiles[strings.TrimSuffix(virtualPath, ".strm")] = path
		} else {
			// Regular file
			result.Symlinks[virtualPath] = path
		}

		return nil
	})

	if err != nil {
		slog.ErrorContext(ctx, "Error during import directory file scan", "error", err)
		return nil, err
	}

	return result, nil
}

// removeEmptyDirectories removes empty directories from the library directory
func (lsw *LibrarySyncWorker) removeEmptyDirectories(ctx context.Context) (int, error) {
	cfg := lsw.configGetter()
	if cfg.Health.LibraryDir == nil || *cfg.Health.LibraryDir == "" {
		return 0, fmt.Errorf("library directory is not configured")
	}

	libraryDir := *cfg.Health.LibraryDir
	slog.InfoContext(ctx, "Starting empty directory cleanup", "library_dir", libraryDir)

	// Helper function to get directory depth
	getDepth := func(path string) int {
		return strings.Count(path, string(filepath.Separator))
	}

	// Collect all directories
	var dirs []string
	err := filepath.WalkDir(libraryDir, func(path string, d os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // Continue on errors
		}

		if d.IsDir() && path != libraryDir {
			dirs = append(dirs, path)
		}

		return nil
	})

	if err != nil {
		slog.ErrorContext(ctx, "Error during directory scan", "error", err)
		return 0, err
	}

	// Sort by depth (deepest first)
	sort.Slice(dirs, func(i, j int) bool {
		return getDepth(dirs[i]) > getDepth(dirs[j])
	})

	// Iteratively remove empty directories
	deletedCount := 0
	maxIterations := 10 // Prevent infinite loops
	for range maxIterations {
		removedThisIteration := 0

		for _, dir := range dirs {
			select {
			case <-ctx.Done():
				return deletedCount, ctx.Err()
			default:
			}

			// Try to remove the directory
			if err := os.Remove(dir); err != nil {
				// Directory not empty or permission error - skip silently
				continue
			}

			slog.InfoContext(ctx, "Removed empty directory", "path", dir)
			removedThisIteration++
			deletedCount++
		}

		// If no directories were removed, we're done
		if removedThisIteration == 0 {
			break
		}
	}

	slog.InfoContext(ctx, "Empty directory cleanup completed",
		"deleted_count", deletedCount)

	return deletedCount, nil
}

// syncMetadataOnly performs a simplified sync for NONE import strategy
// It only synchronizes database records with metadata files, skipping all
// library directory scanning and cleanup operations
func (lsw *LibrarySyncWorker) syncMetadataOnly(ctx context.Context, startTime time.Time) {
	cfg := lsw.configGetter()
	slog.InfoContext(ctx, "Starting metadata-only sync")

	// Initialize progress tracking
	defer lsw.initializeProgressTracking(startTime)()

	// Get all metadata files from filesystem
	metadataFiles, err := lsw.getAllMetadataFiles(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.ErrorContext(ctx, "Failed to get metadata files", "error", err)
		}
		return
	}

	// Update total files count
	lsw.progressMu.Lock()
	lsw.progress.TotalFiles = len(metadataFiles)
	lsw.progressMu.Unlock()

	// Get all health check paths from database
	dbRecords, err := lsw.healthRepo.GetAllHealthCheckRecords(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get automatic health check paths from database", "error", err)
		return
	}

	// Build lookup maps for efficient searching
	syncMaps := lsw.buildSyncMaps(metadataFiles, dbRecords)
	metaFileSet := syncMaps.metaFileSet
	dbPathSet := syncMaps.dbPathSet

	// Find files to add (in filesystem but not in database)
	var filesToAdd []database.AutomaticHealthCheckRecord
	var filesToAddMu sync.Mutex

	// Get concurrency setting (default to 10 if not set)
	concurrency := cfg.Health.LibrarySyncConcurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	// Create a worker pool for parallel metadata reading
	p := pool.New().WithMaxGoroutines(concurrency)

	for mountRelativePath := range metaFileSet {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Capture loop variable for goroutine
		path := mountRelativePath

		p.Go(func() {
			// Check if needs to be added
			if _, exists := dbPathSet[path]; !exists {
				// Read metadata to get release date
				fileMeta, err := lsw.metadataService.ReadFileMetadata(path)
				if err != nil {
					slog.ErrorContext(ctx, "Failed to read metadata",
						"mount_relative_path", path,
						"error", err)
					return
				}

				if fileMeta == nil {
					return
				}

				// Use CreatedAt if ReleaseDate is missing
				releaseDate := fileMeta.ReleaseDate
				if releaseDate == 0 {
					releaseDate = fileMeta.CreatedAt
					// Update metadata file with the CreatedAt as release date
					fileMeta.ReleaseDate = releaseDate
					if err := lsw.metadataService.WriteFileMetadata(path, fileMeta); err != nil {
						slog.ErrorContext(ctx, "Failed to update metadata with release date",
							"path", path,
							"error", err)
					} else {
						slog.InfoContext(ctx, "Set release date from CreatedAt",
							"path", path,
							"release_date", time.Unix(releaseDate, 0))
					}
				}

				// Convert Unix timestamp to time.Time
				releaseDateAsTime := time.Unix(releaseDate, 0)

				// Calculate initial check time
				scheduledCheckAt := calculateInitialCheck(releaseDateAsTime)

				// For NONE strategy, library path is always nil
				// since files are accessed directly via mount
				var libraryPath *string = nil

				// Protect shared slice with mutex
				filesToAddMu.Lock()
				filesToAdd = append(filesToAdd, database.AutomaticHealthCheckRecord{
					FilePath:         path,
					LibraryPath:      libraryPath,
					ReleaseDate:      releaseDateAsTime,
					ScheduledCheckAt: scheduledCheckAt,
					SourceNzbPath:    &fileMeta.SourceNzbPath,
				})
				filesToAddMu.Unlock()
			}

			if lsw.progress != nil {
				lsw.progress.ProcessedFiles.Add(1)
			}
		})
	}

	// Wait for all workers to complete
	p.Wait()

	// Find files to delete (in database but not in filesystem)
	// Pass nil for filesInUse since metadata-only sync doesn't check library usage
	filesToDelete := lsw.findFilesToDelete(ctx, dbRecords, metaFileSet, nil)

	// Perform batch operations
	dbCounts := lsw.syncDatabaseRecords(ctx, filesToAdd, filesToDelete)

	// Record sync results (no cleanup operations for metadata-only sync)
	cleanup := cleanupCounts{
		metadataDeleted:     0,
		libraryFilesDeleted: 0,
		libraryDirsDeleted:  0,
	}
	lsw.recordSyncResult(ctx, startTime, dbCounts, cleanup, len(metadataFiles), len(dbRecords))
}
