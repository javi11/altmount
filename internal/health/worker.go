package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	PendingManualChecks    int          `json:"pending_manual_checks"`
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

	slog.InfoContext(ctx, "Health worker started successfully", "check_interval", hw.getCheckInterval(), "max_concurrent_jobs", 1)
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

	stats := hw.stats
	stats.PendingManualChecks = 0 // No manual queue anymore

	return stats
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

			if err := hw.runHealthCheckCycle(ctx); err != nil {
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

	// Handle the result
	if err := hw.handleHealthCheckResult(ctx, event); err != nil {
		slog.ErrorContext(ctx, "Failed to handle health check result", "file_path", filePath, "error", err)
		return fmt.Errorf("failed to handle health check result: %w", err)
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

// handleHealthCheckResult handles the result of a health check
func (hw *HealthWorker) handleHealthCheckResult(ctx context.Context, event HealthEvent) error {
	switch event.Type {
	case EventTypeFileHealthy:
		// File is now healthy - update metadata
		slog.InfoContext(ctx, "File is healthy", "file_path", event.FilePath)

		// Update metadata status
		if err := hw.metadataService.UpdateFileStatus(event.FilePath, metapb.FileStatus_FILE_STATUS_HEALTHY); err != nil {
			slog.ErrorContext(ctx, "Failed to update metadata status", "file_path", event.FilePath, "error", err)
			return fmt.Errorf("failed to update metadata status: %w", err)
		}

		// Get file health record to calculate next scheduled check
		fileHealth, err := hw.healthRepo.GetFileHealth(ctx, event.FilePath)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to get file health record", "file_path", event.FilePath, "error", err)
			return fmt.Errorf("failed to get file health record: %w", err)
		}

		if fileHealth != nil {
			releaseDate := fileHealth.ReleaseDate
			if releaseDate == nil {
				releaseDate = &fileHealth.CreatedAt
			}

			// Mark as healthy and reschedule next check based on release date
			nextCheck := calculateNextCheck(*releaseDate, time.Now())
			if err := hw.healthRepo.MarkAsHealthy(ctx, event.FilePath, nextCheck); err != nil {
				slog.ErrorContext(ctx, "Failed to mark file as healthy", "file_path", event.FilePath, "error", err)
				return fmt.Errorf("failed to mark file as healthy: %w", err)
			}
			slog.InfoContext(ctx, "File marked as healthy with cleared retry state",
				"file_path", event.FilePath,
				"next_check", nextCheck)
		} else {
			slog.WarnContext(ctx, "File is healthy but no release date available, cannot schedule next check",
				"file_path", event.FilePath)
		}

	case EventTypeFileCorrupted, EventTypeCheckFailed:
		// Get current health record to check retry counts
		fileHealth, err := hw.healthRepo.GetFileHealth(ctx, event.FilePath)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to get file health record", "file_path", event.FilePath, "error", err)
			return fmt.Errorf("failed to get file health record: %w", err)
		}
		if fileHealth == nil {
			slog.WarnContext(ctx, "File health record not found", "file_path", event.FilePath)
			return fmt.Errorf("file health record not found for file: %s", event.FilePath)
		}

		var errorMsg *string
		if event.Error != nil {
			errorText := event.Error.Error()
			errorMsg = &errorText
		}

		// Determine the current phase based on status
		switch fileHealth.Status {
		case database.HealthStatusRepairTriggered:
			// We're in repair phase - handle repair retry logic
			if event.Type == EventTypeFileCorrupted {
				slog.WarnContext(ctx, "Repair attempt failed, file still corrupted",
					"file_path", event.FilePath,
					"repair_retry_count", fileHealth.RepairRetryCount,
					"max_repair_retries", fileHealth.MaxRepairRetries)
			} else {
				slog.ErrorContext(ctx, "Repair check failed", "file_path", event.FilePath, "error", event.Error)
			}

			if err := hw.healthRepo.IncrementRepairRetryCount(ctx, event.FilePath, errorMsg); err != nil {
				slog.ErrorContext(ctx, "Failed to increment repair retry count", "file_path", event.FilePath, "error", err)
				return fmt.Errorf("failed to increment repair retry count: %w", err)
			}

			if fileHealth.RepairRetryCount >= fileHealth.MaxRepairRetries-1 {
				// Max repair retries reached - mark as permanently corrupted
				if err := hw.healthRepo.MarkAsCorrupted(ctx, event.FilePath, errorMsg); err != nil {
					slog.ErrorContext(ctx, "Failed to mark file as corrupted after repair retries", "error", err)
					return fmt.Errorf("failed to mark file as corrupted: %w", err)
				}
				slog.ErrorContext(ctx, "File permanently marked as corrupted after repair retries exhausted", "file_path", event.FilePath)
			} else {
				slog.InfoContext(ctx, "Repair retry scheduled",
					"file_path", event.FilePath,
					"repair_retry_count", fileHealth.RepairRetryCount+1,
					"max_repair_retries", fileHealth.MaxRepairRetries)
			}

		default:
			// We're in health check phase - handle health check retry logic
			if event.Type == EventTypeFileCorrupted {
				slog.WarnContext(ctx, "File still corrupted",
					"file_path", event.FilePath,
					"retry_count", fileHealth.RetryCount,
					"max_retries", fileHealth.MaxRetries)
			} else {
				slog.ErrorContext(ctx, "Health check failed", "file_path", event.FilePath, "error", event.Error)
			}

			// Increment health check retry count
			if err := hw.healthRepo.IncrementRetryCount(ctx, event.FilePath, errorMsg); err != nil {
				slog.ErrorContext(ctx, "Failed to increment retry count", "file_path", event.FilePath, "error", err)
				return fmt.Errorf("failed to increment retry count: %w", err)
			}

			if fileHealth.RetryCount >= fileHealth.MaxRetries-1 {
				// Max health check retries reached - trigger repair phase
				if err := hw.triggerFileRepair(ctx, event.FilePath, errorMsg); err != nil {
					slog.ErrorContext(ctx, "Failed to trigger repair", "error", err)
					return fmt.Errorf("failed to trigger repair: %w", err)
				}
				slog.InfoContext(ctx, "Health check retries exhausted, repair triggered", "file_path", event.FilePath)
			} else {
				slog.InfoContext(ctx, "Health check retry scheduled",
					"file_path", event.FilePath,
					"retry_count", fileHealth.RetryCount+1,
					"max_retries", fileHealth.MaxRetries)
			}
		}
	}

	return nil
}

// processRepairNotification processes a file that needs repair notification to ARRs
func (hw *HealthWorker) processRepairNotification(ctx context.Context, fileHealth *database.FileHealth) error {
	// Check if context is cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	slog.InfoContext(ctx, "Notifying ARRs for repair", "file_path", fileHealth.FilePath, "source_nzb", fileHealth.SourceNzbPath)

	// Use triggerFileRepair to handle the actual ARR notification logic
	// This will directly query ARR APIs to find which instance manages this file
	err := hw.triggerFileRepair(ctx, fileHealth.FilePath, nil)
	if err != nil {
		// If triggerFileRepair fails, increment repair retry count for later retry
		slog.WarnContext(ctx, "Repair trigger failed, will retry later", "file_path", fileHealth.FilePath, "error", err)

		errorMsg := err.Error()
		retryErr := hw.healthRepo.IncrementRepairRetryCount(ctx, fileHealth.FilePath, &errorMsg)
		if retryErr != nil {
			return fmt.Errorf("failed to increment repair retry count after trigger failure: %w", retryErr)
		}

		slog.InfoContext(ctx, "Repair notification retry scheduled",
			"file_path", fileHealth.FilePath,
			"repair_retry_count", fileHealth.RepairRetryCount+1,
			"max_repair_retries", fileHealth.MaxRepairRetries,
			"error", err)

		return nil // Don't return error - retry was scheduled
	}

	slog.InfoContext(ctx, "Repair notification completed successfully", "file_path", fileHealth.FilePath)

	return nil
}

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

	now := time.Now()
	hw.updateStats(func(s *WorkerStats) {
		s.CurrentRunStartTime = &now
		s.CurrentRunFilesChecked = 0
	})

	// Get files due for checking (ordered by scheduled_check_at)
	// Hardcoded to 1 - process one file at a time
	unhealthyFiles, err := hw.healthRepo.GetUnhealthyFiles(ctx, 1)
	if err != nil {
		return fmt.Errorf("failed to get unhealthy files: %w", err)
	}

	// Get files that need repair notifications
	// Hardcoded to 1 - process one file at a time
	repairFiles, err := hw.healthRepo.GetFilesForRepairNotification(ctx, 1)
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
		"max_concurrent_jobs", 1)

	// Process files in parallel using conc
	wg := conc.NewWaitGroup()

	// Process health check files
	for _, fileHealth := range unhealthyFiles {
		wg.Go(func() {
			slog.InfoContext(ctx, "Checking unhealthy file", "file_path", fileHealth.FilePath)

			// Set checking status
			err := hw.healthRepo.SetFileChecking(ctx, fileHealth.FilePath)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to set file checking status", "file_path", fileHealth.FilePath, "error", err)
				return
			}

			// Use performDirectCheck which provides cancellation infrastructure
			err = hw.performDirectCheck(ctx, fileHealth.FilePath)
			if err != nil {
				slog.ErrorContext(ctx, "Health check failed", "file_path", fileHealth.FilePath, "error", err)
				// performDirectCheck already handled the result and stats
			}

			// Update cycle progress stats (performDirectCheck updates individual file stats)
			hw.updateStats(func(s *WorkerStats) {
				s.CurrentRunFilesChecked++
			})
		})
	}

	// Process repair notification files
	for _, fileHealth := range repairFiles {
		wg.Go(func() {
			slog.InfoContext(ctx, "Processing repair notification for file", "file_path", fileHealth.FilePath)

			err := hw.processRepairNotification(ctx, fileHealth)
			if err != nil {
				slog.ErrorContext(ctx, "Repair notification failed", "file_path", fileHealth.FilePath, "error", err)
			}

			// Update cycle progress stats
			hw.updateStats(func(s *WorkerStats) {
				s.CurrentRunFilesChecked++
			})
		})
	}

	// Wait for all files to complete processing
	wg.Wait()

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
	intervalSeconds := hw.configGetter().Health.CheckIntervalSeconds
	if intervalSeconds <= 0 {
		return 5 * time.Second // Default
	}
	return time.Duration(intervalSeconds) * time.Second
}

// triggerFileRepair handles the business logic for triggering repair of a corrupted file
// It directly queries ARR APIs to find which instance manages the file and triggers repair
func (hw *HealthWorker) triggerFileRepair(ctx context.Context, filePath string, errorMsg *string) error {
	slog.InfoContext(ctx, "Triggering file repair using direct ARR API approach", "file_path", filePath)

	healthRecord, err := hw.healthRepo.GetFileHealth(ctx, filePath)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get health record for library path lookup",
			"file_path", filePath,
			"error", err)

		return fmt.Errorf("failed to get health record for library path lookup: %w", err)
	}

	if healthRecord.LibraryPath == nil || *healthRecord.LibraryPath == "" {
		slog.ErrorContext(ctx, "No library path found for file",
			"file_path", filePath)

		return fmt.Errorf("no library path found for file: %s, trigger a manual library sync to fix this", filePath)
	}

	// Step 4: Trigger rescan through the ARR service
	err = hw.arrsService.TriggerFileRescan(ctx, *healthRecord.LibraryPath)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to trigger ARR rescan",
			"file_path", filePath,
			"library_path", *healthRecord.LibraryPath,
			"error", err)

		// If we can't trigger repair, mark as corrupted for manual investigation
		errMsg := err.Error()
		return hw.healthRepo.SetCorrupted(ctx, filePath, &errMsg)
	}

	// ARR rescan was triggered successfully - set repair triggered status
	slog.InfoContext(ctx, "Successfully triggered ARR rescan for file repair",
		"file_path", filePath,
		"library_path", *healthRecord.LibraryPath)

	return nil
}
