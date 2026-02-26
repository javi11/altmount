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
	"github.com/javi11/altmount/internal/pathutil"
	"github.com/sourcegraph/conc"
)

// ARRsRepairService abstracts the ARR repair operations needed by HealthWorker.
type ARRsRepairService interface {
	TriggerFileRescan(ctx context.Context, pathForRescan string, relativePath string) error
}

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
	arrsService     ARRsRepairService
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
	arrsService ARRsRepairService,
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

	if !hw.configGetter().GetHealthEnabled() {
		slog.WarnContext(ctx, "Health worker is disabled via configuration, not starting")
		return nil
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

	// Reset pending files that exhausted retries so they can be rechecked
	if err := hw.healthRepo.ResetStalePendingFiles(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to reset stale pending files during initialization", "error", err)
		// Don't fail startup for this - just log and continue
	}

	// Start the main worker goroutine
	hw.wg.Go(func() {
		hw.run(ctx)
	})

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

// prepareUpdateForResult decides what DB update and side effects are needed based on the check result.
func (hw *HealthWorker) prepareUpdateForResult(ctx context.Context, fh *database.FileHealth, event HealthEvent) (*database.HealthStatusUpdate, func() error) {
	update := &database.HealthStatusUpdate{
		FilePath: fh.FilePath,
	}

	var sideEffect func() error

	if event.Type == EventTypeFileRemoved {
		update.Skip = true
		sideEffect = func() error {
			slog.InfoContext(ctx, "File removed — health record already deleted, skipping bulk update", "file_path", fh.FilePath)
			return nil
		}
		return update, sideEffect
	}

	if event.Type == EventTypeFileHealthy {
		// File is now healthy
		releaseDate := fh.ReleaseDate
		if releaseDate == nil {
			releaseDate = &fh.CreatedAt
		}

		nextCheck := CalculateNextCheck(releaseDate.UTC(), time.Now().UTC())
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
			update.Type = database.UpdateTypeRepairTrigger
			update.Status = database.HealthStatusRepairTriggered
			sideEffect = func() error {
				slog.InfoContext(ctx, "Health check retries exhausted, triggering repair", "file_path", fh.FilePath)
				outcome, err := hw.triggerFileRepair(ctx, fh, errorMsg, event.Details)
				applyRepairOutcome(update, outcome, err)
				return nil
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

// prepareRepairNotificationUpdate builds the update and side effect for a file already in
// repair_triggered state. It re-triggers ARR directly without calling CheckFile, since the
// metadata has already been moved to the corrupted folder.
func (hw *HealthWorker) prepareRepairNotificationUpdate(ctx context.Context, fh *database.FileHealth) (*database.HealthStatusUpdate, func() error) {
	update := &database.HealthStatusUpdate{
		FilePath: fh.FilePath,
	}

	if fh.RepairRetryCount >= fh.MaxRepairRetries-1 {
		// Retries exhausted — give up and mark corrupted.
		update.Type = database.UpdateTypeCorrupted
		update.Status = database.HealthStatusCorrupted
		sideEffect := func() error {
			slog.ErrorContext(ctx, "File permanently marked as corrupted after repair retries exhausted",
				"file_path", fh.FilePath,
				"repair_retry_count", fh.RepairRetryCount)
			return nil
		}
		return update, sideEffect
	}

	// Re-trigger ARR and increment repair_retry_count.
	update.Type = database.UpdateTypeRepairRetry
	update.Status = database.HealthStatusRepairTriggered

	sideEffect := func() error {
		outcome, err := hw.retriggerFileRepair(ctx, fh)
		applyRepairOutcome(update, outcome, err)
		return nil
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

	// Get current file state first to determine check options
	fh, err := hw.healthRepo.GetFileHealth(ctx, filePath)
	if err != nil {
		return fmt.Errorf("failed to get file health state: %w", err)
	}
	if fh == nil {
		return fmt.Errorf("file health record not found: %s", filePath)
	}

	opts := CheckOptions{}
	// Delegate to HealthChecker
	event := hw.healthChecker.CheckFile(checkCtx, filePath, opts)

	// Check if cancelled during check
	select {
	case <-checkCtx.Done():
		return checkCtx.Err()
	default:
	}

	updatePtr, sideEffect := hw.prepareUpdateForResult(ctx, fh, event)
	if sideEffect != nil {
		if err := sideEffect(); err != nil {
			slog.ErrorContext(ctx, "Side effect failed in direct check", "file_path", filePath, "error", err)
		}
	}

	if !updatePtr.Skip {
		if err := hw.healthRepo.UpdateHealthStatusBulk(ctx, []database.HealthStatusUpdate{*updatePtr}); err != nil {
			return fmt.Errorf("failed to update health status: %w", err)
		}
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
			opts := CheckOptions{}
			event := hw.healthChecker.CheckFile(ctx, fh.FilePath, opts)

			updatePtr, sideEffect := hw.prepareUpdateForResult(ctx, fh, event)
			if sideEffect != nil {
				if err := sideEffect(); err != nil {
					slog.ErrorContext(ctx, "Failed to execute side effect for health result", "file_path", fh.FilePath, "error", err)
				}
			}

			resultsMu.Lock()
			results = append(results, *updatePtr)
			resultsMu.Unlock()

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

	for _, fileHealth := range repairFiles {
		fh := fileHealth // Capture for closure
		wg.Go(func() {
			slog.InfoContext(ctx, "Re-triggering repair for file", "file_path", fh.FilePath)

			updatePtr, sideEffect := hw.prepareRepairNotificationUpdate(ctx, fh)

			if sideEffect != nil {
				if err := sideEffect(); err != nil {
					slog.ErrorContext(ctx, "Failed to execute side effect for repair notification", "file_path", fh.FilePath, "error", err)
				}
			}

			resultsMu.Lock()
			results = append(results, *updatePtr)
			resultsMu.Unlock()

			// Update cycle progress stats
			hw.updateStats(func(s *WorkerStats) {
				s.CurrentRunFilesChecked++
				s.TotalFilesChecked++
			})
		})
	}

	// Wait for all files to complete processing
	wg.Wait()

	// Build list of protected directories (categories and complete dir)
	cfg := hw.configGetter()
	protected := []string{"complete", "corrupted_metadata"} // Protect 'complete' and safety folder
	if cfg.SABnzbd.CompleteDir != "" {
		protected = append(protected, filepath.Base(cfg.SABnzbd.CompleteDir))
	}
	for _, cat := range cfg.SABnzbd.Categories {
		protected = append(protected, cat.Name)
		if cat.Dir != "" {
			protected = append(protected, cat.Dir)
		}
	}

	// Clean up empty directories in metadata (e.g. from moved/imported files)
	if err := hw.metadataService.CleanupEmptyDirectories("", protected); err != nil {
		slog.WarnContext(ctx, "Failed to cleanup empty directories in metadata", "error", err)
	}

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

// repairOutcome describes the result of a repair trigger attempt.
type repairOutcome int

const (
	repairOutcomeTriggered repairOutcome = iota // ARR accepted the repair; metadata moved to corrupted folder
	repairOutcomeCorrupted                      // ARR failed with a generic error; mark file corrupted
	repairOutcomeDeleted                        // Health record and/or metadata were deleted (zombie)
)

// applyRepairOutcome maps a repairOutcome to the corresponding fields on the HealthStatusUpdate.
func applyRepairOutcome(update *database.HealthStatusUpdate, outcome repairOutcome, err error) {
	switch outcome {
	case repairOutcomeDeleted:
		update.Skip = true
	case repairOutcomeCorrupted:
		update.Type = database.UpdateTypeCorrupted
		update.Status = database.HealthStatusCorrupted
		if err != nil {
			errMsg := err.Error()
			update.ErrorMessage = &errMsg
		}
	}
}

// resolvePathForRescan determines the absolute path that ARR should rescan for a given file.
// It checks LibraryPath first, then ImportDir, and falls back to MountPath.
func (hw *HealthWorker) resolvePathForRescan(item *database.FileHealth) string {
	if item.LibraryPath != nil && *item.LibraryPath != "" {
		return *item.LibraryPath
	}
	cfg := hw.configGetter()
	if cfg.Import.ImportDir != nil && *cfg.Import.ImportDir != "" {
		return pathutil.JoinAbsPath(*cfg.Import.ImportDir, item.FilePath)
	}
	return pathutil.JoinAbsPath(cfg.MountPath, item.FilePath)
}

// cleanupZombieRecord deletes the health record and associated metadata for a file that is
// no longer tracked by ARR (zombie or orphan). Errors are logged but not returned because
// cleanup is best-effort.
func (hw *HealthWorker) cleanupZombieRecord(ctx context.Context, filePath string) {
	if delErr := hw.healthRepo.DeleteHealthRecord(ctx, filePath); delErr != nil {
		slog.ErrorContext(ctx, "Failed to delete health record during cleanup", "file_path", filePath, "error", delErr)
	}

	cfg := hw.configGetter()
	relativePath := strings.TrimPrefix(filePath, cfg.MountPath)
	relativePath = strings.TrimPrefix(relativePath, "/")

	deleteSourceNzb := false
	if cfg.Metadata.DeleteSourceNzbOnRemoval != nil {
		deleteSourceNzb = *cfg.Metadata.DeleteSourceNzbOnRemoval
	}
	if delMetaErr := hw.metadataService.DeleteFileMetadataWithSourceNzb(ctx, relativePath, deleteSourceNzb); delMetaErr != nil {
		slog.ErrorContext(ctx, "Failed to delete metadata during cleanup", "file_path", filePath, "error", delMetaErr)
	}
}

// triggerFileRepair handles the business logic for triggering repair of a corrupted file.
// It contacts ARR APIs and moves metadata, but does NOT write health status to the DB directly.
// Callers must apply the returned outcome to the HealthStatusUpdate before the bulk DB write.
func (hw *HealthWorker) triggerFileRepair(ctx context.Context, item *database.FileHealth, errorMsg *string, errorDetails *string) (repairOutcome, error) {
	filePath := item.FilePath

	// Check if file metadata still exists. If not, the file is gone (likely upgraded/deleted by Sonarr already)
	// and this health record is a zombie.
	{
		meta, err := hw.metadataService.ReadFileMetadata(filePath)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to read metadata during repair trigger", "file_path", filePath, "error", err)
			// Continue with repair attempt if read failed (could be transient)
		} else if meta == nil {
			slog.WarnContext(ctx, "File metadata missing during repair trigger - file likely deleted/upgraded externally. Cleaning up zombie record.",
				"file_path", filePath)

			if delErr := hw.healthRepo.DeleteHealthRecord(ctx, filePath); delErr != nil {
				slog.ErrorContext(ctx, "Failed to delete zombie health record", "error", delErr)
				return repairOutcomeDeleted, delErr
			}
			return repairOutcomeDeleted, nil
		}
	}

	slog.InfoContext(ctx, "Triggering file repair using direct ARR API approach", "file_path", filePath)

	pathForRescan := hw.resolvePathForRescan(item)

	err := hw.arrsService.TriggerFileRescan(ctx, pathForRescan, filePath)
	if err != nil {
		if errors.Is(err, arrs.ErrEpisodeAlreadySatisfied) || errors.Is(err, arrs.ErrPathMatchFailed) {
			slog.WarnContext(ctx, "File no longer tracked by ARR, removing from AltMount",
				"file_path", filePath, "arr_error", err)
			hw.cleanupZombieRecord(ctx, filePath)
			return repairOutcomeDeleted, nil
		}

		slog.ErrorContext(ctx, "Failed to trigger ARR rescan",
			"file_path", filePath,
			"path_for_rescan", pathForRescan,
			"error", err)
		return repairOutcomeCorrupted, err
	}

	// ARR rescan was triggered successfully.
	slog.InfoContext(ctx, "Successfully triggered ARR rescan for file repair",
		"file_path", filePath,
		"path_for_rescan", pathForRescan)

	// Move the metadata file to the corrupted folder so FUSE/WebDAV stops showing it.
	// We do this AFTER ARR accepts the repair so the file stays visible if ARR cannot find a replacement.
	cfg := hw.configGetter()
	relativePath := strings.TrimPrefix(filePath, cfg.MountPath)
	relativePath = strings.TrimPrefix(relativePath, "/")
	slog.InfoContext(ctx, "Moving metadata file for corrupted item to safety folder to trigger replacement", "file_path", filePath)
	if moveErr := hw.metadataService.MoveToCorrupted(ctx, relativePath); moveErr != nil {
		slog.WarnContext(ctx, "Failed to move corrupted metadata file, proceeding with repair trigger status", "error", moveErr)
	}

	return repairOutcomeTriggered, nil
}

// retriggerFileRepair re-triggers the ARR rescan for a file already in repair_triggered state.
// Unlike triggerFileRepair it does NOT move metadata (already moved) and does NOT write to the DB.
// Callers must apply the returned outcome to the HealthStatusUpdate before the bulk DB write.
func (hw *HealthWorker) retriggerFileRepair(ctx context.Context, item *database.FileHealth) (repairOutcome, error) {
	filePath := item.FilePath
	pathForRescan := hw.resolvePathForRescan(item)

	slog.InfoContext(ctx, "Re-triggering ARR rescan for file in repair", "file_path", filePath, "path_for_rescan", pathForRescan)

	err := hw.arrsService.TriggerFileRescan(ctx, pathForRescan, filePath)
	if err != nil {
		if errors.Is(err, arrs.ErrEpisodeAlreadySatisfied) || errors.Is(err, arrs.ErrPathMatchFailed) {
			slog.WarnContext(ctx, "File no longer tracked by ARR during re-trigger, removing from AltMount", "file_path", filePath)
			hw.cleanupZombieRecord(ctx, filePath)
			return repairOutcomeDeleted, nil
		}

		slog.ErrorContext(ctx, "Failed to re-trigger ARR rescan", "file_path", filePath, "error", err)
		return repairOutcomeCorrupted, err
	}

	slog.InfoContext(ctx, "Successfully re-triggered ARR rescan", "file_path", filePath)
	return repairOutcomeTriggered, nil
}
