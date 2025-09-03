package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/sourcegraph/conc"
	"golift.io/starr"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
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
	TotalFilesRecovered    int64        `json:"total_files_recovered"`
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
	mediaRepo       *database.MediaRepository
	metadataService *metadata.MetadataService
	configGetter    config.ConfigGetter
	logger          *slog.Logger

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
	mediaRepo *database.MediaRepository,
	metadataService *metadata.MetadataService,
	configGetter config.ConfigGetter,
	logger *slog.Logger,
) *HealthWorker {
	return &HealthWorker{
		healthChecker:   healthChecker,
		healthRepo:      healthRepo,
		mediaRepo:       mediaRepo,
		metadataService: metadataService,
		configGetter:    configGetter,
		logger:          logger,
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

	if !hw.getEnabled() {
		hw.logger.Info("Health worker is disabled in configuration")
		return nil
	}

	hw.running = true
	hw.status = WorkerStatusStarting
	hw.updateStats(func(s *WorkerStats) {
		s.Status = WorkerStatusStarting
		s.LastError = nil
	})

	hw.logger.Info("Starting health worker",
		"check_interval", hw.getCheckInterval(),
		"max_concurrent_jobs", hw.getMaxConcurrentJobs())

	// Initialize health system - reset any files stuck in 'checking' status
	if err := hw.healthRepo.ResetFileAllChecking(); err != nil {
		hw.logger.Error("Failed to reset checking files during initialization", "error", err)
		// Don't fail startup for this - just log and continue
	} else {
		hw.logger.Info("Health system initialized - reset any files from 'checking' to 'pending' status")
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

	hw.logger.Info("Health worker started successfully")
	return nil
}

// Stop gracefully stops the health worker
func (hw *HealthWorker) Stop() error {
	hw.mu.Lock()
	defer hw.mu.Unlock()

	if !hw.running {
		return fmt.Errorf("health worker not running")
	}

	hw.status = WorkerStatusStopping
	hw.updateStats(func(s *WorkerStats) {
		s.Status = WorkerStatusStopping
	})

	hw.logger.Info("Stopping health worker...")
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

	hw.logger.Info("Health worker stopped")
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
func (hw *HealthWorker) CancelHealthCheck(filePath string) error {
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
	err := hw.healthRepo.UpdateFileHealth(filePath, database.HealthStatusPending, nil, nil, nil)
	if err != nil {
		hw.logger.Error("Failed to update file status after cancellation", "file_path", filePath, "error", err)
		return fmt.Errorf("failed to update file status after cancellation: %w", err)
	}

	hw.logger.Info("Health check cancelled", "file_path", filePath)
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
			hw.logger.Info("Health worker stopped by context")
			return
		case <-hw.stopChan:
			hw.logger.Info("Health worker stopped by stop signal")
			return
		case <-ticker.C:
			// Check if a cycle is already running
			hw.mu.RLock()
			isCycleRunning := hw.cycleRunning
			hw.mu.RUnlock()

			if isCycleRunning {
				hw.logger.Debug("Skipping health check cycle - previous cycle still running")
				continue
			}

			if err := hw.runHealthCheckCycle(ctx); err != nil {
				hw.logger.Error("Health check cycle failed", "error", err)
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
func (hw *HealthWorker) AddToHealthCheck(filePath string, sourceNzb *string) error {
	// Check if file already exists in health database
	existingHealth, err := hw.healthRepo.GetFileHealth(filePath)
	if err != nil {
		return fmt.Errorf("failed to check existing health record: %w", err)
	}

	// If file doesn't exist in health database, add it
	if existingHealth == nil {
		err = hw.healthRepo.UpdateFileHealth(
			filePath,
			database.HealthStatusPending, // Start as pending - will be checked in next cycle
			nil,
			sourceNzb,
			nil,
		)
		if err != nil {
			return fmt.Errorf("failed to add file to health database: %w", err)
		}

		hw.logger.Info("Added file to health check list", "file_path", filePath)
	} else {
		// File already exists, just reset to pending status if not already pending
		if existingHealth.Status != database.HealthStatusPending {
			err = hw.healthRepo.UpdateFileHealth(
				filePath,
				database.HealthStatusPending,
				nil,
				sourceNzb,
				nil,
			)
			if err != nil {
				return fmt.Errorf("failed to update file status to pending: %w", err)
			}
			hw.logger.Info("Reset file status to pending for health check", "file_path", filePath)
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
		ctx := context.Background() // Use background context for async operation
		checkErr := hw.performDirectCheck(ctx, filePath)
		if checkErr != nil {
			if errors.Is(checkErr, context.Canceled) {
				hw.logger.Info("Background health check canceled", "file_path", filePath)
			} else {
				hw.logger.Error("Background health check failed", "file_path", filePath, "error", checkErr)
			}

			// Get current health record to preserve source NZB path
			fileHealth, getErr := hw.healthRepo.GetFileHealth(filePath)
			var sourceNzb *string
			if getErr == nil && fileHealth != nil {
				sourceNzb = fileHealth.SourceNzbPath
			}

			// Set status back to pending if the check failed
			errorMsg := checkErr.Error()
			updateErr := hw.healthRepo.UpdateFileHealth(filePath, database.HealthStatusPending, &errorMsg, sourceNzb, nil)
			if updateErr != nil {
				hw.logger.Error("Failed to update status after failed check", "file_path", filePath, "error", updateErr)
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
		hw.logger.Error("Failed to handle health check result", "file_path", filePath, "error", err)
		return fmt.Errorf("failed to handle health check result: %w", err)
	}

	// Notify rclone VFS about the status change
	hw.healthChecker.notifyRcloneVFS(filePath, event)

	// Update stats
	hw.updateStats(func(s *WorkerStats) {
		s.TotalFilesChecked++
		switch event.Type {
		case EventTypeFileRecovered:
			s.TotalFilesRecovered++
		case EventTypeFileCorrupted:
			s.TotalFilesCorrupted++
		}
	})

	hw.logger.Info("Direct health check completed", "file_path", filePath, "status", event.Status, "type", event.Type)

	return nil
}

// handleHealthCheckResult handles the result of a health check
func (hw *HealthWorker) handleHealthCheckResult(ctx context.Context, event HealthEvent) error {
	switch event.Type {
	case EventTypeFileRecovered:
		// File is now healthy - update metadata and delete from health database
		hw.logger.Info("File recovered", "file_path", event.FilePath)

		// Update metadata status
		if err := hw.metadataService.UpdateFileStatus(event.FilePath, metapb.FileStatus_FILE_STATUS_HEALTHY); err != nil {
			hw.logger.Error("Failed to update metadata status", "file_path", event.FilePath, "error", err)
			return fmt.Errorf("failed to update metadata status: %w", err)
		}

		// Delete health record since file is now healthy
		if err := hw.healthRepo.DeleteHealthRecord(event.FilePath); err != nil {
			hw.logger.Error("Failed to delete health record for recovered file", "file_path", event.FilePath, "error", err)
			return fmt.Errorf("failed to delete health record: %w", err)
		}
		hw.logger.Info("Removed health record for recovered file", "file_path", event.FilePath)

	case EventTypeFileCorrupted, EventTypeCheckFailed:
		// Get current health record to check retry counts
		fileHealth, err := hw.healthRepo.GetFileHealth(event.FilePath)
		if err != nil {
			hw.logger.Error("Failed to get file health record", "file_path", event.FilePath, "error", err)
			return fmt.Errorf("failed to get file health record: %w", err)
		}
		if fileHealth == nil {
			hw.logger.Warn("File health record not found", "file_path", event.FilePath)
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
				hw.logger.Warn("Repair attempt failed, file still corrupted",
					"file_path", event.FilePath,
					"repair_retry_count", fileHealth.RepairRetryCount,
					"max_repair_retries", fileHealth.MaxRepairRetries)
			} else {
				hw.logger.Error("Repair check failed", "file_path", event.FilePath, "error", event.Error)
			}

			if err := hw.healthRepo.IncrementRepairRetryCount(event.FilePath, errorMsg); err != nil {
				hw.logger.Error("Failed to increment repair retry count", "file_path", event.FilePath, "error", err)
				return fmt.Errorf("failed to increment repair retry count: %w", err)
			}

			if fileHealth.RepairRetryCount >= fileHealth.MaxRepairRetries-1 {
				// Max repair retries reached - mark as permanently corrupted
				if err := hw.healthRepo.MarkAsCorrupted(event.FilePath, errorMsg); err != nil {
					hw.logger.Error("Failed to mark file as corrupted after repair retries", "error", err)
					return fmt.Errorf("failed to mark file as corrupted: %w", err)
				}
				hw.logger.Error("File permanently marked as corrupted after repair retries exhausted", "file_path", event.FilePath)
			} else {
				hw.logger.Info("Repair retry scheduled",
					"file_path", event.FilePath,
					"repair_retry_count", fileHealth.RepairRetryCount+1,
					"max_repair_retries", fileHealth.MaxRepairRetries)
			}

		default:
			// We're in health check phase - handle health check retry logic
			if event.Type == EventTypeFileCorrupted {
				hw.logger.Warn("File still corrupted",
					"file_path", event.FilePath,
					"retry_count", fileHealth.RetryCount,
					"max_retries", fileHealth.MaxRetries)
			} else {
				hw.logger.Error("Health check failed", "file_path", event.FilePath, "error", event.Error)
			}

			// Increment health check retry count
			if err := hw.healthRepo.IncrementRetryCount(event.FilePath, errorMsg); err != nil {
				hw.logger.Error("Failed to increment retry count", "file_path", event.FilePath, "error", err)
				return fmt.Errorf("failed to increment retry count: %w", err)
			}

			if fileHealth.RetryCount >= fileHealth.MaxRetries-1 {
				// Max health check retries reached - trigger repair phase
				if err := hw.triggerFileRepair(ctx, event.FilePath, errorMsg); err != nil {
					hw.logger.Error("Failed to trigger repair", "error", err)
					return fmt.Errorf("failed to trigger repair: %w", err)
				}
				hw.logger.Info("Health check retries exhausted, repair triggered", "file_path", event.FilePath)
			} else {
				hw.logger.Info("Health check retry scheduled",
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

	hw.logger.Info("Notifying ARRs for repair", "file_path", fileHealth.FilePath, "source_nzb", fileHealth.SourceNzbPath)

	// Use triggerFileRepair to handle the actual ARR notification logic
	// This will check if the file exists in media_files and trigger repair if available
	err := hw.triggerFileRepair(ctx, fileHealth.FilePath, nil)
	if err != nil {
		// If triggerFileRepair fails, increment repair retry count for later retry
		hw.logger.Warn("Repair trigger failed, will retry later", "file_path", fileHealth.FilePath, "error", err)

		errorMsg := err.Error()
		retryErr := hw.healthRepo.IncrementRepairRetryCount(fileHealth.FilePath, &errorMsg)
		if retryErr != nil {
			return fmt.Errorf("failed to increment repair retry count after trigger failure: %w", retryErr)
		}

		hw.logger.Info("Repair notification retry scheduled",
			"file_path", fileHealth.FilePath,
			"repair_retry_count", fileHealth.RepairRetryCount+1,
			"max_repair_retries", fileHealth.MaxRepairRetries,
			"error", err)

		return nil // Don't return error - retry was scheduled
	}

	hw.logger.Info("Repair notification completed successfully", "file_path", fileHealth.FilePath)

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

	// Get unhealthy files that need checking
	unhealthyFiles, err := hw.healthRepo.GetUnhealthyFiles(hw.getMaxConcurrentJobs())
	if err != nil {
		return fmt.Errorf("failed to get unhealthy files: %w", err)
	}

	// Get files that need repair notifications
	repairFiles, err := hw.healthRepo.GetFilesForRepairNotification(hw.getMaxConcurrentJobs())
	if err != nil {
		return fmt.Errorf("failed to get files for repair notification: %w", err)
	}

	totalFiles := len(unhealthyFiles) + len(repairFiles)
	if totalFiles == 0 {
		hw.logger.Debug("No unhealthy files found, skipping health check cycle")
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

	hw.logger.Info("Found files to process",
		"health_check_files", len(unhealthyFiles),
		"repair_notification_files", len(repairFiles),
		"total", totalFiles,
		"max_concurrent_jobs", hw.getMaxConcurrentJobs())

	// Process files in parallel using conc
	wg := conc.NewWaitGroup()

	// Process health check files
	for _, fileHealth := range unhealthyFiles {
		wg.Go(func() {
			hw.logger.Info("Checking unhealthy file", "file_path", fileHealth.FilePath)

			// Set checking status
			err := hw.healthRepo.SetFileChecking(fileHealth.FilePath)
			if err != nil {
				hw.logger.Error("Failed to set file checking status", "file_path", fileHealth.FilePath, "error", err)
				return
			}

			// Use performDirectCheck which provides cancellation infrastructure
			err = hw.performDirectCheck(ctx, fileHealth.FilePath)
			if err != nil {
				hw.logger.Error("Health check failed", "file_path", fileHealth.FilePath, "error", err)
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
			hw.logger.Info("Processing repair notification for file", "file_path", fileHealth.FilePath)

			err := hw.processRepairNotification(ctx, fileHealth)
			if err != nil {
				hw.logger.Error("Repair notification failed", "file_path", fileHealth.FilePath, "error", err)
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

	hw.logger.Info("Health check cycle completed",
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
func (hw *HealthWorker) getEnabled() bool {
	return *hw.configGetter().Health.Enabled
}

func (hw *HealthWorker) getCheckInterval() time.Duration {
	interval := hw.configGetter().Health.CheckInterval
	if interval <= 0 {
		return 30 * time.Minute // Default
	}
	return interval
}

func (hw *HealthWorker) getMaxConcurrentJobs() int {
	jobs := hw.configGetter().Health.MaxConcurrentJobs
	if jobs <= 0 {
		return 1 // Default
	}
	return jobs
}

// triggerFileRepair handles the business logic for triggering repair of a corrupted file
// It uses media files to determine the ARR instance and notifies the appropriate service
func (hw *HealthWorker) triggerFileRepair(ctx context.Context, filePath string, errorMsg *string) error {
	// Get media file information to determine which ARR instance to notify
	mediaFiles, err := hw.mediaRepo.GetMediaFilesByPath(filePath)
	if err != nil {
		hw.logger.Warn("Failed to get media files for repair", "file_path", filePath, "error", err)
		// If we can't find media files, still mark as corrupted for manual investigation
		return hw.healthRepo.SetCorrupted(filePath, errorMsg)
	}

	// If no media files found, mark as corrupted for manual handling
	if len(mediaFiles) == 0 {
		hw.logger.Info("No media files found, marking as corrupted", "file_path", filePath)
		return hw.healthRepo.SetCorrupted(filePath, errorMsg)
	}

	// Try to notify ARR instances for each media file
	var lastErr error
	notificationSuccess := false

	for _, mediaFile := range mediaFiles {
		if mediaFile.InstanceName == "" || mediaFile.InstanceType == "" {
			hw.logger.Warn("Media file has incomplete instance information",
				"file_path", filePath,
				"instance_name", mediaFile.InstanceName,
				"instance_type", mediaFile.InstanceType)
			continue
		}

		// Try to notify this ARR instance
		err := hw.notifyARRInstance(ctx, &mediaFile)
		if err != nil {
			hw.logger.Error("Failed to notify ARR instance",
				"file_path", filePath,
				"instance_name", mediaFile.InstanceName,
				"instance_type", mediaFile.InstanceType,
				"error", err)
			lastErr = err
			continue
		}

		notificationSuccess = true
		hw.logger.Info("Successfully notified ARR instance",
			"file_path", filePath,
			"instance_name", mediaFile.InstanceName,
			"instance_type", mediaFile.InstanceType)
	}

	// Update database status based on notification success
	if notificationSuccess {
		// At least one notification succeeded - set repair triggered
		return hw.healthRepo.SetRepairTriggered(filePath, errorMsg)
	} else {
		// All notifications failed - mark as corrupted for manual investigation
		if lastErr != nil {
			errMsg := lastErr.Error()
			return hw.healthRepo.SetCorrupted(filePath, &errMsg)
		}
		return hw.healthRepo.SetCorrupted(filePath, errorMsg)
	}
}

// findConfigInstance finds a specific ARR instance in the configuration
func (hw *HealthWorker) findConfigInstance(instanceType, instanceName string) (*config.ArrsInstanceConfig, error) {
	cfg := hw.configGetter()

	switch instanceType {
	case "radarr":
		for _, instance := range cfg.Arrs.RadarrInstances {
			if instance.Name == instanceName {
				return &instance, nil
			}
		}
	case "sonarr":
		for _, instance := range cfg.Arrs.SonarrInstances {
			if instance.Name == instanceName {
				return &instance, nil
			}
		}
	}

	return nil, fmt.Errorf("instance not found: %s/%s", instanceType, instanceName)
}

// createRadarrClient creates a Radarr client for the given instance
func (hw *HealthWorker) createRadarrClient(instanceConfig *config.ArrsInstanceConfig) (*radarr.Radarr, error) {
	if instanceConfig.URL == "" {
		return nil, fmt.Errorf("Radarr instance URL is empty")
	}
	if instanceConfig.APIKey == "" {
		return nil, fmt.Errorf("Radarr instance API key is empty")
	}

	client := radarr.New(&starr.Config{
		URL:    instanceConfig.URL,
		APIKey: instanceConfig.APIKey,
	})

	return client, nil
}

// createSonarrClient creates a Sonarr client for the given instance
func (hw *HealthWorker) createSonarrClient(instanceConfig *config.ArrsInstanceConfig) (*sonarr.Sonarr, error) {
	if instanceConfig.URL == "" {
		return nil, fmt.Errorf("Sonarr instance URL is empty")
	}
	if instanceConfig.APIKey == "" {
		return nil, fmt.Errorf("Sonarr instance API key is empty")
	}

	client := sonarr.New(&starr.Config{
		URL:    instanceConfig.URL,
		APIKey: instanceConfig.APIKey,
	})

	return client, nil
}

// notifyARRInstance notifies a specific ARR instance about a file that needs repair
func (hw *HealthWorker) notifyARRInstance(ctx context.Context, mediaFile *database.MediaFile) error {
	// Find the instance configuration
	instanceConfig, err := hw.findConfigInstance(mediaFile.InstanceType, mediaFile.InstanceName)
	if err != nil {
		return fmt.Errorf("failed to find instance config: %w", err)
	}

	// Check if instance is enabled
	if instanceConfig.Enabled == nil || !*instanceConfig.Enabled {
		return fmt.Errorf("instance %s/%s is disabled", mediaFile.InstanceType, mediaFile.InstanceName)
	}

	// Create client and trigger rescan based on instance type
	switch mediaFile.InstanceType {
	case "radarr":
		client, err := hw.createRadarrClient(instanceConfig)
		if err != nil {
			return fmt.Errorf("failed to create Radarr client: %w", err)
		}
		return hw.triggerRadarrRescan(ctx, client, mediaFile)

	case "sonarr":
		client, err := hw.createSonarrClient(instanceConfig)
		if err != nil {
			return fmt.Errorf("failed to create Sonarr client: %w", err)
		}
		return hw.triggerSonarrRescan(ctx, client, mediaFile)

	default:
		return fmt.Errorf("unsupported instance type: %s", mediaFile.InstanceType)
	}
}

// triggerRadarrRescan triggers a rescan in Radarr for the given media file
func (hw *HealthWorker) triggerRadarrRescan(ctx context.Context, client *radarr.Radarr, mediaFile *database.MediaFile) error {
	// Check if external ID is available
	if mediaFile.ExternalID == 0 {
		return fmt.Errorf("no external ID available for media file")
	}

	movieID := mediaFile.ExternalID

	hw.logger.Info("Triggering Radarr rescan",
		"instance", mediaFile.InstanceName,
		"movie_id", movieID,
		"file_path", mediaFile.FilePath)

	// Create a refresh movie command
	cmd := &radarr.CommandRequest{
		Name:     "RefreshMovie",
		MovieIDs: []int64{movieID},
	}

	// Send the command to trigger a refresh/rescan of the movie
	// This will make Radarr re-check the file and potentially re-download if corrupted
	response, err := client.SendCommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to trigger Radarr rescan for movie ID %d: %w", movieID, err)
	}

	hw.logger.Info("Successfully triggered Radarr rescan",
		"instance", mediaFile.InstanceName,
		"movie_id", movieID,
		"command_id", response.ID)

	return nil
}

// triggerSonarrRescan triggers a rescan in Sonarr for the given media file
func (hw *HealthWorker) triggerSonarrRescan(ctx context.Context, client *sonarr.Sonarr, mediaFile *database.MediaFile) error {
	// Check if external ID is available
	if mediaFile.ExternalID == 0 {
		return fmt.Errorf("no external ID available for media file")
	}

	episodeID := mediaFile.ExternalID

	hw.logger.Info("Triggering Sonarr rescan",
		"instance", mediaFile.InstanceName,
		"episode_id", episodeID,
		"file_path", mediaFile.FilePath)

	// Create a refresh series command
	cmd := &sonarr.CommandRequest{
		Name:       "RefreshSeries",
		EpisodeIDs: []int64{episodeID},
	}

	// Send the command to trigger a refresh/rescan of the series
	// This will make Sonarr re-check the file and potentially re-download if corrupted
	response, err := client.SendCommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to trigger Sonarr rescan for episode ID %d: %w", episodeID, err)
	}

	hw.logger.Info("Successfully triggered Sonarr rescan",
		"instance", mediaFile.InstanceName,
		"episode_id", episodeID,
		"command_id", response.ID)

	return nil
}
