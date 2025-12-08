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
	TotalFiles     int       `json:"total_files"`
	ProcessedFiles int       `json:"processed_files"`
	StartTime      time.Time `json:"start_time"`
}

// internalSyncProgress is the internal representation using atomic for thread safety
type internalSyncProgress struct {
	TotalFiles     int
	ProcessedFiles atomic.Int32
	StartTime      time.Time
}

// SyncResult stores the results of a completed library sync
type SyncResult struct {
	FilesAdded          int           `json:"files_added"`
	FilesDeleted        int           `json:"files_deleted"`
	MetadataDeleted     int           `json:"metadata_deleted"`
	LibraryFilesDeleted int           `json:"library_files_deleted"`
	SymlinksUpdated     int           `json:"symlinks_updated"`
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
	configManager   *config.Manager
	cancelFunc      context.CancelFunc
	mu              sync.Mutex
	running         bool
	progressMu      sync.RWMutex
	progress        *internalSyncProgress
	lastSyncResult  *SyncResult
	manualTrigger   chan struct{}
	rcloneClient    rclonecli.RcloneRcClient
}

// NewLibrarySyncWorker creates a new library sync worker
func NewLibrarySyncWorker(
	metadataService *metadata.MetadataService,
	healthRepo *database.HealthRepository,
	configGetter config.ConfigGetter,
	configManager *config.Manager,
	rcloneClient rclonecli.RcloneRcClient,
) *LibrarySyncWorker {
	return &LibrarySyncWorker{
		metadataService: metadataService,
		healthRepo:      healthRepo,
		configGetter:    configGetter,
		configManager:   configManager,
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
			ProcessedFiles: int(processedFiles),
			StartTime:      lsw.progress.StartTime,
		}
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
			lsw.safeSyncLibrary(ctx, false)
		case <-lsw.manualTrigger:
			slog.InfoContext(ctx, "Manual library sync trigger received")
			lsw.safeSyncLibrary(ctx, false)
		}
	}
}

// safeSyncLibrary executes SyncLibrary with panic recovery
func (lsw *LibrarySyncWorker) safeSyncLibrary(ctx context.Context, dryRun bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "Panic in library sync", "panic", r)
		}
	}()
	lsw.SyncLibrary(ctx, dryRun)
}

// syncMaps holds the metadata and database record maps used during synchronization
type syncMaps struct {
	metaFileSet map[string]string                              // mount relative path -> metadata file path
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
	symlinksUpdated     int
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
			// If repair is triggered, skip file existence check as it might be temporarily missing during repair
			if dbRecord.Status == database.HealthStatusRepairTriggered {
				continue
			}

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
		SymlinksUpdated:     cleanup.symlinksUpdated,
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
		"symlinks_updated", cleanup.symlinksUpdated,
		"duration", duration)
}

// syncDatabaseRecords performs batch add and delete operations on the health check database
// Returns the number of records added and deleted. If dryRun is true, it only counts without
// performing actual database operations.
func (lsw *LibrarySyncWorker) syncDatabaseRecords(
	ctx context.Context,
	filesToAdd []database.AutomaticHealthCheckRecord,
	filesToDelete []string,
	dryRun bool,
) syncCounts {
	counts := syncCounts{}

	// Batch add new files
	if len(filesToAdd) > 0 {
		if !dryRun {
			if err := lsw.healthRepo.BatchAddAutomaticHealthChecks(ctx, filesToAdd); err != nil {
				slog.ErrorContext(ctx, "Failed to batch add automatic health checks",
					"count", len(filesToAdd),
					"error", err)
			} else {
				counts.added = len(filesToAdd)
				slog.InfoContext(ctx, "Added new files to automatic health checks",
					"count", counts.added)
			}
		} else {
			counts.added = len(filesToAdd)
		}
	}

	// Batch delete orphaned files
	if len(filesToDelete) > 0 {
		if !dryRun {
			if err := lsw.healthRepo.DeleteHealthRecordsBulk(ctx, filesToDelete); err != nil {
				slog.ErrorContext(ctx, "Failed to delete orphaned health records",
					"count", len(filesToDelete),
					"error", err)
			} else {
				counts.deleted = len(filesToDelete)
				slog.InfoContext(ctx, "Deleted orphaned health records",
					"count", counts.deleted)
			}
		} else {
			counts.deleted = len(filesToDelete)
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
	lsw.progress = &internalSyncProgress{
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

// SyncLibrary performs a full library synchronization. If dryRun is true,
// it will count what would be deleted without actually deleting anything,
// and return a DryRunResult. If dryRun is false, it performs the sync normally
// and returns nil.
func (lsw *LibrarySyncWorker) SyncLibrary(ctx context.Context, dryRun bool) *DryRunResult {
	startTime := time.Now()
	cfg := lsw.configGetter()
	slog.InfoContext(ctx, "Starting library sync")

	// Determine mount paths for symlink updates
	var oldMountPath, newMountPath string
	if lsw.configManager != nil && lsw.configManager.NeedsLibrarySync() && !dryRun {
		oldMountPath = lsw.configManager.GetPreviousMountPath()
		newMountPath = cfg.MountPath
		slog.InfoContext(ctx, "Will update symlinks during filesystem walk",
			"old_mount", oldMountPath,
			"new_mount", newMountPath)
	}

	// Check import strategy - if NONE, only sync DB with metadata files
	if cfg.Import.ImportStrategy == config.ImportStrategyNone {
		slog.InfoContext(ctx, "Import strategy is NONE, performing metadata-only sync")
		return lsw.syncMetadataOnly(ctx, startTime, dryRun)
	}

	// Initialize progress tracking
	defer lsw.initializeProgressTracking(startTime)()

	// Parallelize filesystem walks for better performance
	var metadataFiles []string
	var libraryFiles *UsedFiles
	var importDirFiles *UsedFiles
	var librarySymlinksUpdated, importSymlinksUpdated int

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
	// Also updates symlinks inline if mount path has changed
	fsWalkPool.Go(func() error {
		files, updated, err := lsw.getAllLibraryFiles(ctx, oldMountPath, newMountPath)
		if err != nil {
			return fmt.Errorf("failed to get library files: %w", err)
		}
		libraryFiles = files
		librarySymlinksUpdated = updated
		return nil
	})

	// Get all import directory files
	// Also updates symlinks inline if mount path has changed
	fsWalkPool.Go(func() error {
		files, updated, err := lsw.getAllImportDirFiles(ctx, oldMountPath, newMountPath)
		if err != nil {
			return fmt.Errorf("failed to get import directory files: %w", err)
		}
		importDirFiles = files
		importSymlinksUpdated = updated
		return nil
	})

	if err := fsWalkPool.Wait(); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.ErrorContext(ctx, "Failed to walk filesystem", "error", err)
		}
		return nil
	}

	// Log and clear mount path change flag if symlinks were updated
	totalSymlinksUpdated := librarySymlinksUpdated + importSymlinksUpdated
	if totalSymlinksUpdated > 0 && lsw.configManager != nil {
		lsw.configManager.ClearLibrarySyncFlag()
		slog.InfoContext(ctx, "Completed mount path symlink updates during filesystem walk",
			"library_symlinks", librarySymlinksUpdated,
			"import_symlinks", importSymlinksUpdated,
			"total_updated", totalSymlinksUpdated)
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
		return nil
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
	jobChan := make(chan string, concurrency*2)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobChan {
				// Check if needs to be added
				if it, exists := dbPathSet[path]; !exists || it.LibraryPath == nil {
					// Read metadata to get release date
					fileMeta, err := lsw.metadataService.ReadFileMetadata(path)
					if err != nil {
						slog.ErrorContext(ctx, "Failed to read metadata",
							"mount_relative_path", path,
							"error", err)

						// Register as corrupted so HealthWorker can pick it up and trigger repair
						// Look up library path from our map
						libPath := lsw.getLibraryPath(path, filesInUse)
						if libPath != nil {
							regErr := lsw.healthRepo.RegisterCorruptedFile(ctx, path, libPath, err.Error())
							if regErr != nil {
								slog.ErrorContext(ctx, "Failed to register corrupted file", "path", path, "error", regErr)
							}
						}
						continue
					}

					if fileMeta == nil {
						continue
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
					libraryPath := lsw.getLibraryPath(path, filesInUse)
				
					// Protect shared slice with mutex
					filesToAddMu.Lock()
					filesToAdd = append(filesToAdd, database.AutomaticHealthCheckRecord{
						FilePath:         path,
						LibraryPath:      libraryPath,
						ReleaseDate:      &releaseDateAsTime,
						ScheduledCheckAt: &scheduledCheckAt,
						SourceNzbPath:    &fileMeta.SourceNzbPath,
					})
					filesToAddMu.Unlock()
				}

				if lsw.progress != nil {
					lsw.progress.ProcessedFiles.Add(1)
				}
			}
		}()
	}

	// Feed the workers
	go func() {
		for mountRelativePath := range metaFileSet {
			select {
			case <-ctx.Done():
				break
			case jobChan <- mountRelativePath:
			}
		}
		close(jobChan)
	}()

	// Wait for all workers to complete
	wg.Wait()

	// Additional cleanup of orphaned metadata files if enabled
	metadataDeletedCount := 0
	if cfg.Health.CleanupOrphanedFiles != nil && *cfg.Health.CleanupOrphanedFiles {
		// We already have libraryFiles from earlier in the function
		for relativeMountPath := range metaFileSet {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			libraryPath := lsw.getLibraryPath(relativeMountPath, filesInUse)

			if libraryPath == nil {
				if !dryRun {
					err := lsw.metadataService.DeleteFileMetadata(relativeMountPath)
					if err != nil {
						slog.ErrorContext(ctx, "Failed to delete metadata file", "error", err)
						continue
					}
				}
				metadataDeletedCount++
			}
		}

	}

	// Cleanup orphaned library files (symlinks and STRM files without metadata)
	libraryFilesDeletedCount := 0

	if cfg.Health.CleanupOrphanedFiles != nil && *cfg.Health.CleanupOrphanedFiles {
		for metaPath, file := range filesInUse {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			libraryPath := lsw.getLibraryPath(metaPath, filesInUse)

			if libraryPath == nil {
				if !dryRun {
					err := os.Remove(file)
					if err != nil {
						slog.ErrorContext(ctx, "Failed to delete library file", "error", err)
						continue
					}
				}
				libraryFilesDeletedCount++
			}
		}
	}

	// Find files to delete (in database but not in filesystem or not in use)
	filesToDelete := lsw.findFilesToDelete(ctx, dbRecords, metaFileSet, filesInUse)

	// Perform batch operations
	dbCounts := lsw.syncDatabaseRecords(ctx, filesToAdd, filesToDelete, dryRun)

	// Return dry run results or record sync results
	if dryRun {
		wouldCleanup := cfg.Health.CleanupOrphanedFiles != nil && *cfg.Health.CleanupOrphanedFiles
		return &DryRunResult{
			OrphanedMetadataCount:  metadataDeletedCount,
			OrphanedLibraryFiles:   libraryFilesDeletedCount,
			DatabaseRecordsToClean: dbCounts.deleted,
			WouldCleanup:           wouldCleanup,
		}
	}

	// Record sync results
	cleanup := cleanupCounts{
		metadataDeleted:     metadataDeletedCount,
		libraryFilesDeleted: libraryFilesDeletedCount,
		symlinksUpdated:     totalSymlinksUpdated,
	}
	lsw.recordSyncResult(ctx, startTime, dbCounts, cleanup, len(metadataFiles), len(dbRecords))
	return nil
}

// DryRunResult holds the results of a dry run sync
type DryRunResult struct {
	OrphanedMetadataCount  int
	OrphanedLibraryFiles   int
	DatabaseRecordsToClean int
	WouldCleanup           bool
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
// If oldMountPath and newMountPath are provided, it also updates symlinks pointing to the old path
func (lsw *LibrarySyncWorker) getAllLibraryFiles(ctx context.Context, oldMountPath, newMountPath string) (*UsedFiles, int, error) {
	cfg := lsw.configGetter()

	// Get library directory
	if cfg.Health.LibraryDir == nil || *cfg.Health.LibraryDir == "" {
		return nil, 0, fmt.Errorf("library directory is not configured")
	}

	libraryDir := *cfg.Health.LibraryDir
	mountDir := cfg.MountPath

	result := &UsedFiles{
		Symlinks:  make(map[string]string),
		StrmFiles: make(map[string]string),
	}

	symlinkUpdates := 0
	shouldUpdateSymlinks := oldMountPath != "" && newMountPath != "" && oldMountPath != newMountPath
	oldMountPathClean := filepath.Clean(oldMountPath)
	newMountPathClean := filepath.Clean(newMountPath)

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

			// Update symlink if it points to the old mount path
			if shouldUpdateSymlinks && strings.HasPrefix(cleanTarget, oldMountPathClean) {
				// Extract the relative path within the mount
				relativePath := strings.TrimPrefix(cleanTarget, oldMountPathClean)
				relativePath = strings.TrimPrefix(relativePath, string(filepath.Separator))

				// Create new target path
				newTarget := filepath.Join(newMountPathClean, relativePath)

				// Remove the old symlink
				if err := os.Remove(path); err != nil {
					slog.WarnContext(ctx, "Failed to remove old symlink during mount path update",
						"path", path,
						"error", err)
					return nil
				}

				// Create new symlink pointing to new mount path
				if err := os.Symlink(newTarget, path); err != nil {
					slog.ErrorContext(ctx, "Failed to create updated symlink",
						"path", path,
						"old_target", cleanTarget,
						"new_target", newTarget,
						"error", err)
					return nil
				}

				slog.InfoContext(ctx, "Updated symlink to new mount path",
					"path", path,
					"old_target", cleanTarget,
					"new_target", newTarget)

				symlinkUpdates++
				// Update cleanTarget to the new target for storage
				cleanTarget = newTarget
			}

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
		return nil, 0, err
	}

	return result, symlinkUpdates, nil
}

// getAllImportDirFiles collects both regular files and .strm files from import directory in a single pass
func (lsw *LibrarySyncWorker) getAllImportDirFiles(ctx context.Context, oldMountPath, newMountPath string) (*UsedFiles, int, error) {
	cfg := lsw.configGetter()

	// Get import directory
	if cfg.Import.ImportDir == nil || *cfg.Import.ImportDir == "" {
		// No import directory configured - return empty result
		return &UsedFiles{
			Symlinks:  make(map[string]string),
			StrmFiles: make(map[string]string),
		}, 0, nil
	}

	importDir := *cfg.Import.ImportDir

	// Check if directory exists
	if _, err := os.Stat(importDir); os.IsNotExist(err) {
		slog.WarnContext(ctx, "Import directory does not exist", "import_dir", importDir)
		return &UsedFiles{
			Symlinks:  make(map[string]string),
			StrmFiles: make(map[string]string),
		}, 0, nil
	}

	result := &UsedFiles{
		Symlinks:  make(map[string]string),
		StrmFiles: make(map[string]string),
	}

	symlinkUpdates := 0
	shouldUpdateSymlinks := oldMountPath != "" && newMountPath != "" && oldMountPath != newMountPath
	oldMountPathClean := filepath.Clean(oldMountPath)
	newMountPathClean := filepath.Clean(newMountPath)
	mountDir := cfg.MountPath

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

		// Check if it's a symlink
		if d.Type()&os.ModeSymlink != 0 {
			// Read the symlink target
			target, err := os.Readlink(path)
			if err != nil {
				slog.WarnContext(ctx, "Failed to read symlink in import directory",
					"path", path,
					"error", err)
				return nil
			}

			// Make target absolute if it's relative
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}

			// Clean the paths for comparison
			cleanTarget := filepath.Clean(target)
			cleanMountDir := filepath.Clean(mountDir)

			// Update symlink if it points to the old mount path
			if shouldUpdateSymlinks && strings.HasPrefix(cleanTarget, oldMountPathClean) {
				// Extract the relative path within the mount
				relativePath := strings.TrimPrefix(cleanTarget, oldMountPathClean)
				relativePath = strings.TrimPrefix(relativePath, string(filepath.Separator))

				// Create new target path
				newTarget := filepath.Join(newMountPathClean, relativePath)

				// Remove the old symlink
				if err := os.Remove(path); err != nil {
					slog.WarnContext(ctx, "Failed to remove old symlink during mount path update",
						"path", path,
						"error", err)
				} else {
					// Create new symlink pointing to new mount path
					if err := os.Symlink(newTarget, path); err != nil {
						slog.ErrorContext(ctx, "Failed to create updated symlink during mount path update",
							"path", path,
							"old_target", cleanTarget,
							"new_target", newTarget,
							"error", err)
					} else {
						symlinkUpdates++
						slog.DebugContext(ctx, "Updated symlink for new mount path",
							"path", path,
							"old_target", cleanTarget,
							"new_target", newTarget)
						// Update the target for further processing
						cleanTarget = newTarget
					}
				}
			}

			// Validate that the symlink points to mount directory
			if !strings.HasPrefix(cleanTarget, cleanMountDir) {
				slog.WarnContext(ctx, "Symlink in import directory does not point to mount directory",
					"path", path,
					"target", cleanTarget,
					"mount_dir", cleanMountDir)
				return nil
			}

			// Store symlink with relative path as key
			result.Symlinks[virtualPath] = path
		} else if strings.HasSuffix(d.Name(), ".strm") {
			// STRM file - add without .strm extension
			result.StrmFiles[strings.TrimSuffix(virtualPath, ".strm")] = path
		}
		// Ignore all other regular files

		return nil
	})

	if err != nil {
		slog.ErrorContext(ctx, "Error during import directory file scan", "error", err)
		return nil, 0, err
	}

	return result, symlinkUpdates, nil
}

// getLibraryPath looks up the library path for a given mount relative path
// It checks both the full mount path and the relative path (for STRM files)
func (lsw *LibrarySyncWorker) getLibraryPath(metaPath string, filesInUse map[string]string) *string {
	cfg := lsw.configGetter()
	mountPath := filepath.Join(cfg.MountPath, metaPath)

	if libPath, ok := filesInUse[mountPath]; ok {
		return &libPath
	}

	if libPath, ok := filesInUse[metaPath]; ok {
		// Try with virtual path (for STRM files)
		return &libPath
	}

	return nil
}

// syncMetadataOnly performs a simplified sync for NONE import strategy
// It only synchronizes database records with metadata files, skipping all
// library directory scanning and cleanup operations. If dryRun is true,
// it will count what would be changed without actually modifying the database,
// and return a DryRunResult. If dryRun is false, it performs the sync normally
// and returns nil.
func (lsw *LibrarySyncWorker) syncMetadataOnly(ctx context.Context, startTime time.Time, dryRun bool) *DryRunResult {
	cfg := lsw.configGetter()
	slog.InfoContext(ctx, "Starting metadata-only sync")

	// Initialize progress tracking
	defer lsw.initializeProgressTracking(startTime)()

	// Get all metadata files from filesystem
	// OPTIMIZATION: This still loads all metadata paths into memory.
	// Ideally we would stream this too, but for now let's optimize the DB side.
	metadataFiles, err := lsw.getAllMetadataFiles(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.ErrorContext(ctx, "Failed to get metadata files", "error", err)
		}
		return nil
	}

	// Update total files count
	lsw.progressMu.Lock()
	lsw.progress.TotalFiles = len(metadataFiles)
	lsw.progressMu.Unlock()

	// Get all health check paths from database (Memory Optimized: Only paths)
	dbPaths, err := lsw.healthRepo.GetAllHealthCheckPaths(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get automatic health check paths from database", "error", err)
		return nil
	}

	// Build lookup map for efficient searching (Memory Optimized: map[string]struct{})
	// Convert metadata files to map for efficient lookup
	metaFileSet := make(map[string]string, len(metadataFiles))
	for _, path := range metadataFiles {
		mountRelativePath := lsw.metaPathToMountRelativePath(path)
		metaFileSet[mountRelativePath] = path
	}

	// Convert database paths to set for existence check
	dbPathSet := make(map[string]struct{}, len(dbPaths))
	for _, path := range dbPaths {
		dbPathSet[path] = struct{}{}
	}

	// Channel for streaming inserts to avoid holding all new records in memory
	insertChan := make(chan database.AutomaticHealthCheckRecord, 100)
	var dbAddedCount atomic.Int32
	var insertWg sync.WaitGroup

	// Start background batch inserter
	insertWg.Add(1)
	go func() {
		defer insertWg.Done()
		batchSize := 200
		batch := make([]database.AutomaticHealthCheckRecord, 0, batchSize)

		flushBatch := func() {
			if len(batch) > 0 {
				if !dryRun {
					if err := lsw.healthRepo.BatchAddAutomaticHealthChecks(ctx, batch); err != nil {
						slog.ErrorContext(ctx, "Failed to batch add automatic health checks",
							"count", len(batch),
							"error", err)
					} else {
						dbAddedCount.Add(int32(len(batch)))
					}
				} else {
					dbAddedCount.Add(int32(len(batch)))
				}
				// Clear batch but keep capacity
				batch = batch[:0]
			}
		}

		for record := range insertChan {
			batch = append(batch, record)
			if len(batch) >= batchSize {
				flushBatch()
			}
		}
		// Flush remaining items
		flushBatch()
	}()

	// Get concurrency setting (default to 10 if not set)
	concurrency := cfg.Health.LibrarySyncConcurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	// Create a worker pool for parallel metadata reading
	jobChan := make(chan string, concurrency*2)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobChan {
				// Check if needs to be added
				                if _, exists := dbPathSet[path]; !exists {
									// Read metadata to get release date
									fileMeta, err := lsw.metadataService.ReadFileMetadata(path)
									if err != nil {
										slog.ErrorContext(ctx, "Failed to read metadata",
											"mount_relative_path", path,
											"error", err)
				
										// Register as corrupted so HealthWorker can pick it up and trigger repair
										// For NONE strategy, library path is effectively the mount path or nil
										regErr := lsw.healthRepo.RegisterCorruptedFile(ctx, path, nil, err.Error())
										if regErr != nil {
											slog.ErrorContext(ctx, "Failed to register corrupted file", "path", path, "error", regErr)
										}
										continue
									}
					if fileMeta == nil {
						continue
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

					// Stream to insert channel
					insertChan <- database.AutomaticHealthCheckRecord{
						FilePath:         path,
						LibraryPath:      libraryPath,
						ReleaseDate:      &releaseDateAsTime,
						ScheduledCheckAt: &scheduledCheckAt,
						SourceNzbPath:    &fileMeta.SourceNzbPath,
					}
				}

				if lsw.progress != nil {
					lsw.progress.ProcessedFiles.Add(1)
				}
			}
		}()
	}

	// Feed the workers
	go func() {
		for mountRelativePath := range metaFileSet {
			select {
			case <-ctx.Done():
				break
			case jobChan <- mountRelativePath:
			}
		}
		close(jobChan)
	}()

	// Wait for all workers to complete
	wg.Wait()
	close(insertChan)
	insertWg.Wait() // Wait for batch inserter to finish

	// Find files to delete (in database but not in filesystem)
	// Pass nil for filesInUse since metadata-only sync doesn't check library usage
	// We use dbPaths (slice of strings) directly
	var filesToDelete []string
	for _, dbPath := range dbPaths {
		// Check if metadata file exists
		if _, exists := metaFileSet[dbPath]; !exists {
			filesToDelete = append(filesToDelete, dbPath)
		}
	}

	// Perform deletions
	deletedCount := 0
	if len(filesToDelete) > 0 {
		if !dryRun {
			if err := lsw.healthRepo.DeleteHealthRecordsBulk(ctx, filesToDelete); err != nil {
				slog.ErrorContext(ctx, "Failed to delete orphaned health records",
					"count", len(filesToDelete),
					"error", err)
			} else {
				deletedCount = len(filesToDelete)
				slog.InfoContext(ctx, "Deleted orphaned health records",
					"count", deletedCount)
			}
		} else {
			deletedCount = len(filesToDelete)
		}
	}

	// Return dry run results or record sync results
	if dryRun {
		wouldCleanup := cfg.Health.CleanupOrphanedFiles != nil && *cfg.Health.CleanupOrphanedFiles
		return &DryRunResult{
			OrphanedMetadataCount:  0, // No orphaned metadata in NONE strategy
			OrphanedLibraryFiles:   0, // No library files in NONE strategy
			DatabaseRecordsToClean: deletedCount,
			WouldCleanup:           wouldCleanup,
		}
	}

	// Record sync results (no cleanup operations for metadata-only sync)
	dbCounts := syncCounts{
		added:   int(dbAddedCount.Load()),
		deleted: deletedCount,
	}
	cleanup := cleanupCounts{
		metadataDeleted:     0,
		libraryFilesDeleted: 0,
		symlinksUpdated:     0,
	}
	lsw.recordSyncResult(ctx, startTime, dbCounts, cleanup, len(metadataFiles), len(dbPaths))
	return nil
}
