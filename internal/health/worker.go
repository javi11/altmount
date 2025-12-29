package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/sourcegraph/conc"
)

// WorkerStatus represents the current status of the health worker
type WorkerStatus string

const (
	WorkerStatusStopped  WorkerStatus = "stopped"
	WorkerStatusStarting WorkerStatus = "starting"
	WorkerStatusRunning  WorkerStatus = "running"
	WorkerStatusStopping WorkerStatus = "stopping"
)

// WorkerStats represents statistics about the health worker
type WorkerStats struct {
	Status                 WorkerStatus `json:"status"`
	LastRunTime            *time.Time   `json:"last_run_time,omitempty"`
	NextRunTime            *time.Time   `json:"next_run_time,omitempty"`
	TotalRunsCompleted     int64        `json:"total_runs_completed"`
	TotalFilesChecked      int64        `json:"total_files_checked"`
	TotalFilesHealthy      int64        `json:"total_files_healthy"`
	TotalFilesCorrupted    int64        `json:"total_files_corrupted"`
	CurrentRunStartTime    *time.Time   `json:"current_run_start_time,omitempty"`
	CurrentRunFilesChecked int          `json:"current_run_files_checked"`
	LastError              *string      `json:"last_error,omitempty"`
	ErrorCount             int64        `json:"error_count"`
}

// HealthWorker manages continuous health monitoring and manual check requests
type HealthWorker struct {
	healthChecker   *HealthChecker
	healthRepo      *database.HealthRepository
	metadataService *metadata.MetadataService
	arrsService     *arrs.Service
	configGetter    config.ConfigGetter

	// Worker state
	status       WorkerStatus
	running      bool
	cycleRunning bool // Flag to prevent overlapping cycles
	stopChan     chan struct{}
	wg           sync.WaitGroup
	mu           sync.RWMutex

	// Active checks tracking for cancellation
	activeChecks   map[string]context.CancelFunc // filePath -> cancel function
	activeChecksMu sync.RWMutex

	// Statistics
	stats   WorkerStats
	statsMu sync.RWMutex
}

// NewHealthWorker creates a new health worker
func NewHealthWorker(
	healthChecker *HealthChecker,
	healthRepo *database.HealthRepository,
	metadataService *metadata.MetadataService,
	arrsService *arrs.Service,
	configGetter config.ConfigGetter,
) *HealthWorker {
	return &HealthWorker{
		healthChecker:   healthChecker,
		healthRepo:      healthRepo,
		metadataService: metadataService,
		arrsService:     arrsService,
		configGetter:    configGetter,
		status:          WorkerStatusStopped,
		stopChan:        make(chan struct{}),
		activeChecks:    make(map[string]context.CancelFunc),
		stats: WorkerStats{
			Status: WorkerStatusStopped,
		},
	}
}

// Start begins the health worker service
func (hw *HealthWorker) Start(ctx context.Context) error {
	hw.mu.Lock()
	defer hw.mu.Unlock()

	if hw.running {
		return fmt.Errorf("health worker already running")
	}
	hw.running = true
	hw.status = WorkerStatusStarting
	hw.updateStats(func(s *WorkerStats) {
		s.Status = WorkerStatusStarting
		s.LastError = nil
	})

	// Initialize health system - reset any files stuck in 'checking' status
	if err := hw.healthRepo.ResetFileAllChecking(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to reset checking files during initialization", "error", err)
		// Don't fail startup for this - just log and continue
	}

	// Start the main worker goroutine
	hw.wg.Add(1)
	go func() {
		defer hw.wg.Done()
		hw.run(ctx)
	}()

	hw.status = WorkerStatusRunning
	hw.updateStats(func(s *WorkerStats) {
		s.Status = WorkerStatusRunning
	})

	slog.InfoContext(ctx, "Health worker started successfully", "check_interval", hw.getCheckInterval(), "max_concurrent_jobs", hw.getMaxConcurrentJobs())
	return nil
}

// Stop gracefully stops the health worker
func (hw *HealthWorker) Stop(ctx context.Context) error {
	hw.mu.Lock()
	defer hw.mu.Unlock()

	if !hw.running {
		return fmt.Errorf("health worker not running")
	}

	hw.status = WorkerStatusStopping
	hw.updateStats(func(s *WorkerStats) {
		s.Status = WorkerStatusStopping
	})

	slog.InfoContext(ctx, "Stopping health worker...")
	close(hw.stopChan)
	hw.running = false

	// Wait for all goroutines to finish
	hw.wg.Wait()

	hw.status = WorkerStatusStopped
	hw.updateStats(func(s *WorkerStats) {
		s.Status = WorkerStatusStopped
		s.CurrentRunStartTime = nil
		s.CurrentRunFilesChecked = 0
	})

	slog.InfoContext(ctx, "Health worker stopped")
	return nil
}

// IsRunning returns whether the health worker is currently running
func (hw *HealthWorker) IsRunning() bool {
	hw.mu.RLock()
	defer hw.mu.RUnlock()
	return hw.running
}

// GetStatus returns the current worker status
func (hw *HealthWorker) GetStatus() WorkerStatus {
	hw.mu.RLock()
	defer hw.mu.RUnlock()
	return hw.status
}

// GetStats returns current worker statistics
func (hw *HealthWorker) GetStats() WorkerStats {
	hw.statsMu.RLock()
	defer hw.statsMu.RUnlock()

	return hw.stats
}

// CancelHealthCheck cancels an active health check for the specified file
func (hw *HealthWorker) CancelHealthCheck(ctx context.Context, filePath string) error {
	hw.activeChecksMu.Lock()
	defer hw.activeChecksMu.Unlock()

	cancelFunc, exists := hw.activeChecks[filePath]
	if !exists {
		return fmt.Errorf("no active health check found for file: %s", filePath)
	}

	// Cancel the context
	cancelFunc()

	// Remove from active checks
	delete(hw.activeChecks, filePath)

	// Update file status to pending to allow retry
	err := hw.healthRepo.UpdateFileHealth(ctx, filePath, database.HealthStatusPending, nil, nil, nil, false)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update file status after cancellation", "file_path", filePath, "error", err)
		return fmt.Errorf("failed to update file status after cancellation: %w", err)
	}

	slog.InfoContext(ctx, "Health check cancelled", "file_path", filePath)
	return nil
}

// IsCheckActive returns whether a health check is currently active for the specified file
func (hw *HealthWorker) IsCheckActive(filePath string) bool {
	hw.activeChecksMu.RLock()
	defer hw.activeChecksMu.RUnlock()

	_, exists := hw.activeChecks[filePath]
	return exists
}

// IsCycleRunning returns whether a health check cycle is currently running
func (hw *HealthWorker) IsCycleRunning() bool {
	hw.mu.RLock()
	defer hw.mu.RUnlock()
	return hw.cycleRunning
}

// run is the main worker loop
func (hw *HealthWorker) run(ctx context.Context) {
	ticker := time.NewTicker(hw.getCheckInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "Health worker stopped by context")
			return
		case <-hw.stopChan:
			slog.InfoContext(ctx, "Health worker stopped by stop signal")
			return
		case <-ticker.C:
			// Check if a cycle is already running
			hw.mu.RLock()
			isCycleRunning := hw.cycleRunning
			hw.mu.RUnlock()

			if isCycleRunning {
				slog.DebugContext(ctx, "Skipping health check cycle - previous cycle still running")
				continue
			}

			if err := hw.safeRunHealthCheckCycle(ctx); err != nil {
				slog.ErrorContext(ctx, "Health check cycle failed", "error", err)
				hw.updateStats(func(s *WorkerStats) {
					s.ErrorCount++
					errMsg := err.Error()
					s.LastError = &errMsg
				})
			}
		}
	}
}

// safeRunHealthCheckCycle runs a health check cycle with panic recovery
func (hw *HealthWorker) safeRunHealthCheckCycle(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in health check cycle: %v", r)
			slog.ErrorContext(ctx, "Panic in health check cycle", "panic", r)
		}
	}()
	return hw.runHealthCheckCycle(ctx)
}

// AddToHealthCheck adds a file to the health check list with pending status
func (hw *HealthWorker) AddToHealthCheck(ctx context.Context, filePath string, sourceNzb *string) error {
	// Check if file already exists in health database
	existingHealth, err := hw.healthRepo.GetFileHealth(ctx, filePath)
	if err != nil {
		return fmt.Errorf("failed to check existing health record: %w", err)
	}

	// If file doesn't exist in health database, add it
	if existingHealth == nil {
		err = hw.healthRepo.UpdateFileHealth(ctx,
			filePath,
			database.HealthStatusPending, // Start as pending - will be checked in next cycle
			nil,
			sourceNzb,
			nil,
			false,
		)
		if err != nil {
			return fmt.Errorf("failed to add file to health database: %w", err)
		}

		slog.InfoContext(ctx, "Added file to health check list", "file_path", filePath)
	} else {
		// File already exists, just reset to pending status if not already pending
		if existingHealth.Status != database.HealthStatusPending {
			err = hw.healthRepo.UpdateFileHealth(ctx,
				filePath,
				database.HealthStatusPending,
				nil,
				sourceNzb,
				nil,
				false,
			)
			if err != nil {
				return fmt.Errorf("failed to update file status to pending: %w", err)
			}
			slog.InfoContext(ctx, "Reset file status to pending for health check", "file_path", filePath)
		}
	}

	return nil
}

// PerformBackgroundCheck starts a health check in background and returns immediately
func (hw *HealthWorker) PerformBackgroundCheck(ctx context.Context, filePath string) error {
	if !hw.IsRunning() {
		return fmt.Errorf("health worker is not running")
	}

	// Start health check in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		checkErr := hw.performDirectCheck(ctx, filePath)
		if checkErr != nil {
			if errors.Is(checkErr, context.DeadlineExceeded) {
				slog.ErrorContext(ctx, "Background health check timed out after 10 minutes", "file_path", filePath)
			} else {
				slog.ErrorContext(ctx, "Background health check failed", "file_path", filePath, "error", checkErr)
			}

			// Get current health record to preserve source NZB path
			fileHealth, getErr := hw.healthRepo.GetFileHealth(ctx, filePath)
			var sourceNzb *string
			if getErr == nil && fileHealth != nil {
				sourceNzb = fileHealth.SourceNzbPath
			}

			// Set status back to pending if the check failed
			errorMsg := checkErr.Error()
			updateErr := hw.healthRepo.UpdateFileHealth(ctx, filePath, database.HealthStatusPending, &errorMsg, sourceNzb, nil, false)
			if updateErr != nil {
				slog.ErrorContext(ctx, "Failed to update status after failed check", "file_path", filePath, "error", updateErr)
			}
		}
	}()

	return nil
}

// prepareUpdateForResult decides what DB update and side effects are needed based on the check result
func (hw *HealthWorker) prepareUpdateForResult(ctx context.Context, fh *database.FileHealth, event HealthEvent) (database.HealthStatusUpdate, func() error) {
	update := database.HealthStatusUpdate{
		FilePath: fh.FilePath,
	}

	var sideEffect func() error

	if event.Type == EventTypeFileHealthy {
		// File is now healthy
		releaseDate := fh.ReleaseDate
		if releaseDate == nil {
			releaseDate = &fh.CreatedAt
		}

		nextCheck := CalculateNextCheck(*releaseDate, time.Now().UTC())
		update.Type = database.UpdateTypeHealthy
		update.Status = database.HealthStatusHealthy
		update.ScheduledCheckAt = nextCheck

		sideEffect = func() error {
			slog.InfoContext(ctx, "File is healthy", "file_path", fh.FilePath)
			return hw.metadataService.UpdateFileStatus(fh.FilePath, metapb.FileStatus_FILE_STATUS_HEALTHY)
		}

		return update, sideEffect
	}

	// Handle Corrupted or CheckFailed
	var errorMsg *string
	if event.Error != nil {
		text := event.Error.Error()
		errorMsg = &text
	}
	update.ErrorMessage = errorMsg
	update.ErrorDetails = event.Details

	switch fh.Status {
	case database.HealthStatusRepairTriggered:
		if fh.RepairRetryCount >= fh.MaxRepairRetries-1 {
			update.Type = database.UpdateTypeCorrupted
			update.Status = database.HealthStatusCorrupted
			sideEffect = func() error {
				slog.ErrorContext(ctx, "File permanently marked as corrupted after repair retries exhausted", "file_path", fh.FilePath)
				return nil
			}
		} else {
			update.Type = database.UpdateTypeRepairRetry
			update.Status = database.HealthStatusRepairTriggered
			sideEffect = func() error {
				slog.InfoContext(ctx, "Repair retry scheduled",
					"file_path", fh.FilePath,
					"repair_retry_count", fh.RepairRetryCount+1)
				return nil
			}
		}

	default:
		// Regular health check phase
		if fh.RetryCount >= fh.MaxRetries-1 {
			// Trigger repair phase
			update.Type = database.UpdateTypeRepairRetry // This will set status to repair_triggered
			update.Status = database.HealthStatusRepairTriggered
			sideEffect = func() error {
				slog.InfoContext(ctx, "Health check retries exhausted, triggering repair", "file_path", fh.FilePath)
				return hw.triggerFileRepair(ctx, fh, errorMsg, event.Details)
			}
		} else {
			// Increment health check retry count
			backoffMinutes := 15 * (1 << fh.RetryCount)
			nextCheck := time.Now().UTC().Add(time.Duration(backoffMinutes) * time.Minute)

			update.Type = database.UpdateTypeRetry
			update.Status = database.HealthStatusPending
			update.ScheduledCheckAt = nextCheck

			sideEffect = func() error {
				slog.InfoContext(ctx, "Health check retry scheduled",
					"file_path", fh.FilePath,
					"retry_count", fh.RetryCount+1,
					"next_check", nextCheck)
				return nil
			}
		}
	}

	return update, sideEffect
}

// performDirectCheck performs a health check on a single file using the HealthChecker
func (hw *HealthWorker) performDirectCheck(ctx context.Context, filePath string) error {
	// Create cancellable context for this check
	checkCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Track active check
	hw.activeChecksMu.Lock()
	hw.activeChecks[filePath] = cancel
	hw.activeChecksMu.Unlock()

	// Ensure cleanup on exit
	defer func() {
		hw.activeChecksMu.Lock()
		delete(hw.activeChecks, filePath)
		hw.activeChecksMu.Unlock()
	}()

	// Check if already cancelled
	select {
	case <-checkCtx.Done():
		return checkCtx.Err()
	default:
	}

	// Delegate to HealthChecker
	event := hw.healthChecker.CheckFile(checkCtx, filePath)

	// Check if cancelled during check
	select {
	case <-checkCtx.Done():
		return checkCtx.Err()
	default:
	}

	// Get current file state
	fh, err := hw.healthRepo.GetFileHealth(ctx, filePath)
	if err != nil {
		return fmt.Errorf("failed to get file health state: %w", err)
	}
	if fh == nil {
		return fmt.Errorf("file health record not found: %s", filePath)
	}

	// Prepare result for update
	update, sideEffect := hw.prepareUpdateForResult(ctx, fh, event)

	// Execute side effects
	if sideEffect != nil {
		if err := sideEffect(); err != nil {
			slog.ErrorContext(ctx, "Side effect failed in direct check", "file_path", filePath, "error", err)
		}
	}

	// Perform database update
	if err := hw.healthRepo.UpdateHealthStatusBulk(ctx, []database.HealthStatusUpdate{update}); err != nil {
		return fmt.Errorf("failed to update health status: %w", err)
	}

	// Notify rclone VFS about the status change
	hw.healthChecker.notifyRcloneVFS(filePath, event)

	// Update stats
	hw.updateStats(func(s *WorkerStats) {
		s.TotalFilesChecked++
		switch event.Type {
		case EventTypeFileHealthy:
			s.TotalFilesHealthy++
		case EventTypeFileCorrupted:
			s.TotalFilesCorrupted++
		}
	})

	return nil
}

// updateStats safely updates worker statistics
// runHealthCheckCycle runs a single cycle of health checks
func (hw *HealthWorker) runHealthCheckCycle(ctx context.Context) error {
	// Set the cycle running flag
	hw.mu.Lock()
	hw.cycleRunning = true
	hw.mu.Unlock()

	// Ensure we clear the flag when done
	defer func() {
		hw.mu.Lock()
		hw.cycleRunning = false
		hw.mu.Unlock()
	}()

	now := time.Now().UTC()
	hw.updateStats(func(s *WorkerStats) {
		s.CurrentRunStartTime = &now
		s.CurrentRunFilesChecked = 0
	})

	maxJobs := hw.getMaxConcurrentJobs()

	// Get files due for checking (ordered by scheduled_check_at)
	unhealthyFiles, err := hw.healthRepo.GetUnhealthyFiles(ctx, maxJobs)
	if err != nil {
		return fmt.Errorf("failed to get unhealthy files: %w", err)
	}

	// Get files that need repair notifications
	repairFiles, err := hw.healthRepo.GetFilesForRepairNotification(ctx, maxJobs)
	if err != nil {
		return fmt.Errorf("failed to get files for repair notification: %w", err)
	}

	totalFiles := len(unhealthyFiles) + len(repairFiles)
	if totalFiles == 0 {
		hw.updateStats(func(s *WorkerStats) {
			s.CurrentRunStartTime = nil
			s.CurrentRunFilesChecked = 0
			s.TotalRunsCompleted++
			s.LastRunTime = &now
			nextRun := now.Add(hw.getCheckInterval())
			s.NextRunTime = &nextRun
		})
		return nil
	}

	slog.InfoContext(ctx, "Found files to process",
		"health_check_files", len(unhealthyFiles),
		"repair_notification_files", len(repairFiles),
		"total", totalFiles,
		"max_concurrent_jobs", maxJobs)

	// Process files in parallel using conc
	wg := conc.NewWaitGroup()
	var results []database.HealthStatusUpdate
	var resultsMu sync.Mutex

	// Process health check files
	for _, fileHealth := range unhealthyFiles {
		fh := fileHealth // Capture for closure
		wg.Go(func() {
			slog.InfoContext(ctx, "Checking unhealthy file", "file_path", fh.FilePath)

			// Set checking status
			err := hw.healthRepo.SetFileChecking(ctx, fh.FilePath)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to set file checking status", "file_path", fh.FilePath, "error", err)
				return
			}

			// Perform check
			event := hw.healthChecker.CheckFile(ctx, fh.FilePath)

			// Prepare result for batch update
			update, sideEffect := hw.prepareUpdateForResult(ctx, fh, event)

			resultsMu.Lock()
			results = append(results, update)
			resultsMu.Unlock()

			// Handle non-DB side effects (metadata updates, repair triggers)
			if sideEffect != nil {
				if err := sideEffect(); err != nil {
					slog.ErrorContext(ctx, "Failed to execute side effect for health result", "file_path", fh.FilePath, "error", err)
				}
			}

			// Notify VFS
			hw.healthChecker.notifyRcloneVFS(fh.FilePath, event)

			// Update cycle progress stats
			hw.updateStats(func(s *WorkerStats) {
				s.CurrentRunFilesChecked++
				s.TotalFilesChecked++
				switch event.Type {
				case EventTypeFileHealthy:
					s.TotalFilesHealthy++
				case EventTypeFileCorrupted:
					s.TotalFilesCorrupted++
				}
			})
		})
	}

	// Process repair notification files
	for _, fileHealth := range repairFiles {
		fh := fileHealth // Capture for closure
		wg.Go(func() {
			slog.InfoContext(ctx, "Checking repair status for file", "file_path", fh.FilePath)

			// Perform check
			event := hw.healthChecker.CheckFile(ctx, fh.FilePath)

			// Prepare result for batch update
			update, sideEffect := hw.prepareUpdateForResult(ctx, fh, event)

			resultsMu.Lock()
			results = append(results, update)
			resultsMu.Unlock()

			// Handle side effects
			if sideEffect != nil {
				_ = sideEffect()
			}

			// Notify VFS
			hw.healthChecker.notifyRcloneVFS(fh.FilePath, event)

			// Update cycle progress stats
			hw.updateStats(func(s *WorkerStats) {
				s.CurrentRunFilesChecked++
				s.TotalFilesChecked++
				switch event.Type {
				case EventTypeFileHealthy:
					s.TotalFilesHealthy++
				case EventTypeFileCorrupted:
					s.TotalFilesCorrupted++
				}
			})
		})
	}

	// Wait for all files to complete processing
	wg.Wait()

	// Perform bulk database update
	if len(results) > 0 {
		if err := hw.healthRepo.UpdateHealthStatusBulk(ctx, results); err != nil {
			slog.ErrorContext(ctx, "Failed to perform bulk health status update", "error", err)
		}
	}

	// Update final stats
	hw.updateStats(func(s *WorkerStats) {
		s.CurrentRunStartTime = nil
		s.CurrentRunFilesChecked = 0
		s.TotalRunsCompleted++
		s.LastRunTime = &now
		nextRun := now.Add(hw.getCheckInterval())
		s.NextRunTime = &nextRun
	})

	slog.InfoContext(ctx, "Health check cycle completed",
		"health_check_files", len(unhealthyFiles),
		"repair_notification_files", len(repairFiles),
		"total_files", totalFiles,
		"duration", time.Since(now))

	return nil
}

// updateStats safely updates worker statistics
func (hw *HealthWorker) updateStats(updateFunc func(*WorkerStats)) {
	hw.statsMu.Lock()
	defer hw.statsMu.Unlock()
	updateFunc(&hw.stats)
}

// Helper methods to get dynamic health config values
func (hw *HealthWorker) getCheckInterval() time.Duration {
	return hw.configGetter().GetCheckInterval()
}

func (hw *HealthWorker) getMaxConcurrentJobs() int {
	return hw.configGetter().GetMaxConcurrentJobs()
}

// triggerFileRepair handles the business logic for triggering repair of a corrupted file
// It directly queries ARR APIs to find which instance manages the file and triggers repair
func (hw *HealthWorker) triggerFileRepair(ctx context.Context, item *database.FileHealth, errorMsg *string, errorDetails *string) error {
	filePath := item.FilePath
	slog.InfoContext(ctx, "Triggering file repair using direct ARR API approach", "file_path", filePath)

	cfg := hw.configGetter()

	var pathForRescan string

	if item.LibraryPath != nil && *item.LibraryPath != "" {
		pathForRescan = *item.LibraryPath
	} else if cfg.Import.ImportDir != nil && *cfg.Import.ImportDir != "" {
		pathForRescan = filepath.Join(*cfg.Import.ImportDir, strings.TrimPrefix(filePath, "/"))
	} else {
		pathForRescan = filepath.Join(hw.configGetter().MountPath, strings.TrimPrefix(filePath, "/"))
	}

	// Step 4: Trigger rescan through the ARR service
	err := hw.arrsService.TriggerFileRescan(ctx, pathForRescan, filePath)
	if err != nil {
		if errors.Is(err, arrs.ErrPathMatchFailed) {
			slog.WarnContext(ctx, "File not found in ARR (likely upgraded/deleted), removing orphan from AltMount",
				"file_path", filePath)

			// Delete health record
			if delErr := hw.healthRepo.DeleteHealthRecord(ctx, filePath); delErr != nil {
				slog.ErrorContext(ctx, "Failed to delete orphaned health record", "error", delErr)
			}

			// Delete metadata file
			// We need the relative path for metadata deletion
			relativePath := strings.TrimPrefix(filePath, hw.configGetter().MountPath)
			relativePath = strings.TrimPrefix(relativePath, "/")
			if delMetaErr := hw.metadataService.DeleteFileMetadata(relativePath); delMetaErr != nil {
				slog.ErrorContext(ctx, "Failed to delete orphaned metadata file", "error", delMetaErr)
			}

			return nil
		}

		slog.ErrorContext(ctx, "Failed to trigger ARR rescan",
			"file_path", filePath,
			"path_for_rescan", pathForRescan,
			"error", err)

		// If we can't trigger repair, mark as corrupted for manual investigation
		errMsg := err.Error()
		return hw.healthRepo.SetCorrupted(ctx, filePath, &errMsg, errorDetails)
	}

	// ARR rescan was triggered successfully - set repair triggered status
	slog.InfoContext(ctx, "Successfully triggered ARR rescan for file repair",
		"file_path", filePath,
		"path_for_rescan", pathForRescan)

	// Update status to repair_triggered
	if err := hw.healthRepo.SetRepairTriggered(ctx, filePath, errorMsg, errorDetails); err != nil {
		slog.ErrorContext(ctx, "Failed to set repair_triggered status",
			"file_path", filePath,
			"error", err)
		return fmt.Errorf("failed to set repair_triggered status: %w", err)
	}

	return nil
}
