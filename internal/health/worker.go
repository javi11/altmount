package health

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
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
	Status                 WorkerStatus  `json:"status"`
	LastRunTime            *time.Time    `json:"last_run_time,omitempty"`
	NextRunTime            *time.Time    `json:"next_run_time,omitempty"`
	TotalRunsCompleted     int64         `json:"total_runs_completed"`
	TotalFilesChecked      int64         `json:"total_files_checked"`
	TotalFilesRecovered    int64         `json:"total_files_recovered"`
	TotalFilesCorrupted    int64         `json:"total_files_corrupted"`
	CurrentRunStartTime    *time.Time    `json:"current_run_start_time,omitempty"`
	CurrentRunFilesChecked int           `json:"current_run_files_checked"`
	PendingManualChecks    int           `json:"pending_manual_checks"`
	LastError              *string       `json:"last_error,omitempty"`
	ErrorCount             int64         `json:"error_count"`
}

// ManualCheckRequest represents a request to manually check a file
type ManualCheckRequest struct {
	FilePath    string
	MaxRetries  *int    // Optional override for max retries
	SourceNzb   *string // Optional source NZB path
	Priority    bool    // If true, check immediately instead of queuing
	ResponseCh  chan error // Channel to send response back
}

// HealthWorkerConfig holds configuration for the health worker
type HealthWorkerConfig struct {
	CheckInterval         time.Duration
	MaxConcurrentJobs     int
	BatchSize             int
	MaxRetries            int
	MaxSegmentConnections int
	CheckAllSegments      bool
	Enabled               bool
}

// HealthWorker manages continuous health monitoring and manual check requests
type HealthWorker struct {
	healthRepo      *database.HealthRepository
	metadataService *metadata.MetadataService
	poolManager     pool.Manager
	config          HealthWorkerConfig
	logger          *slog.Logger
	
	// Worker state
	status          WorkerStatus
	running         bool
	stopChan        chan struct{}
	wg              sync.WaitGroup
	mu              sync.RWMutex
	
	// Manual check queue
	manualCheckChan chan ManualCheckRequest
	manualQueue     []ManualCheckRequest
	manualMu        sync.Mutex
	
	// Active checks tracking for cancellation
	activeChecks map[string]context.CancelFunc // filePath -> cancel function
	activeChecksMu sync.RWMutex
	
	// Statistics
	stats WorkerStats
	statsMu sync.RWMutex
}

// NewHealthWorker creates a new health worker
func NewHealthWorker(
	healthRepo *database.HealthRepository,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	config HealthWorkerConfig,
	logger *slog.Logger,
) *HealthWorker {
	// Set defaults if not provided
	if config.CheckInterval == 0 {
		config.CheckInterval = 30 * time.Minute
	}
	if config.MaxConcurrentJobs == 0 {
		config.MaxConcurrentJobs = 3
	}
	if config.BatchSize == 0 {
		config.BatchSize = 10
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 5
	}
	if config.MaxSegmentConnections == 0 {
		config.MaxSegmentConnections = 5
	}

	return &HealthWorker{
		healthRepo:      healthRepo,
		metadataService: metadataService,
		poolManager:     poolManager,
		config:          config,
		logger:          logger,
		status:          WorkerStatusStopped,
		stopChan:        make(chan struct{}),
		manualCheckChan: make(chan ManualCheckRequest, 100), // Buffer for manual requests
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

	if !hw.config.Enabled {
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
		"check_interval", hw.config.CheckInterval,
		"batch_size", hw.config.BatchSize,
		"max_concurrent_jobs", hw.config.MaxConcurrentJobs,
		"max_retries", hw.config.MaxRetries)

	// Start the main worker goroutine
	hw.wg.Add(1)
	go func() {
		defer hw.wg.Done()
		hw.run(ctx)
	}()

	// Start the manual check processor
	hw.wg.Add(1)
	go func() {
		defer hw.wg.Done()
		hw.processManualChecks(ctx)
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
	
	// Add pending manual checks count
	hw.manualMu.Lock()
	pending := len(hw.manualQueue) + len(hw.manualCheckChan)
	hw.manualMu.Unlock()
	
	stats := hw.stats
	stats.PendingManualChecks = pending
	
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

// AddManualCheck adds a file for manual health checking
func (hw *HealthWorker) AddManualCheck(filePath string, maxRetries *int, sourceNzb *string, priority bool) error {
	if !hw.IsRunning() {
		return fmt.Errorf("health worker is not running")
	}

	responseCh := make(chan error, 1)
	request := ManualCheckRequest{
		FilePath:   filePath,
		MaxRetries: maxRetries,
		SourceNzb:  sourceNzb,
		Priority:   priority,
		ResponseCh: responseCh,
	}

	// Try to send the request
	select {
	case hw.manualCheckChan <- request:
		// Wait for response
		select {
		case err := <-responseCh:
			return err
		case <-time.After(30 * time.Second):
			return fmt.Errorf("timeout waiting for manual check response")
		}
	default:
		return fmt.Errorf("manual check queue is full")
	}
}

// run is the main worker loop
func (hw *HealthWorker) run(ctx context.Context) {
	ticker := time.NewTicker(hw.config.CheckInterval)
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

// processManualChecks processes manual check requests
func (hw *HealthWorker) processManualChecks(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-hw.stopChan:
			return
		case request := <-hw.manualCheckChan:
			hw.processManualCheckRequest(ctx, request)
		}
	}
}

// processManualCheckRequest processes a single manual check request
func (hw *HealthWorker) processManualCheckRequest(ctx context.Context, request ManualCheckRequest) {
	defer close(request.ResponseCh)
	
	// Check if file already exists in health database
	existingHealth, err := hw.healthRepo.GetFileHealth(request.FilePath)
	if err != nil {
		request.ResponseCh <- fmt.Errorf("failed to check existing health record: %w", err)
		return
	}

	// If file doesn't exist in health database, add it
	if existingHealth == nil {
		err = hw.healthRepo.UpdateFileHealth(
			request.FilePath,
			database.HealthStatusPending, // Start as pending since it's never been checked
			nil,
			request.SourceNzb,
			nil,
		)
		if err != nil {
			request.ResponseCh <- fmt.Errorf("failed to add file to health database: %w", err)
			return
		}
		
		hw.logger.Info("Added file to health database for manual check", "file_path", request.FilePath)
	}

	// If priority check, process immediately
	if request.Priority {
		if err := hw.performSingleFileCheck(ctx, request.FilePath); err != nil {
			hw.logger.Error("Failed to perform priority health check", "file_path", request.FilePath, "error", err)
			request.ResponseCh <- fmt.Errorf("failed to perform priority health check: %w", err)
			return
		}
	}

	request.ResponseCh <- nil
}

// PerformDirectCheck performs an immediate health check on a single file (public method)
func (hw *HealthWorker) PerformDirectCheck(ctx context.Context, filePath string) error {
	return hw.performSingleFileCheck(ctx, filePath)
}

// performSingleFileCheck performs a health check on a single file
func (hw *HealthWorker) performSingleFileCheck(ctx context.Context, filePath string) error {
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
	
	// Get file metadata
	fileMeta, err := hw.metadataService.ReadFileMetadata(filePath)
	if err != nil {
		hw.logger.Error("Failed to read file metadata for manual check", "file_path", filePath, "error", err)
		return fmt.Errorf("failed to read file metadata: %w", err)
	}
	if fileMeta == nil {
		hw.logger.Warn("File metadata not found for manual check", "file_path", filePath)
		return fmt.Errorf("file metadata not found for file: %s", filePath)
	}

	// Check if cancelled before proceeding
	select {
	case <-checkCtx.Done():
		return checkCtx.Err()
	default:
	}

	// Create a health checker for this single check
	checkerConfig := HealthCheckerConfig{
		MaxConcurrentJobs:     1,
		BatchSize:             1,
		MaxRetries:            hw.config.MaxRetries,
		MaxSegmentConnections: hw.config.MaxSegmentConnections,
		CheckAllSegments:      hw.config.CheckAllSegments,
	}

	checker := NewHealthChecker(hw.healthRepo, hw.metadataService, hw.poolManager, checkerConfig)
	
	// Perform the check with cancellable context
	event := checker.checkSingleFile(checkCtx, filePath, fileMeta)
	
	// Check if cancelled during check
	select {
	case <-checkCtx.Done():
		return checkCtx.Err()
	default:
	}
	
	// Get current health record
	fileHealth, err := hw.healthRepo.GetFileHealth(filePath)
	if err != nil {
		hw.logger.Error("Failed to get file health record", "file_path", filePath, "error", err)
		return fmt.Errorf("failed to get file health record: %w", err)
	}
	if fileHealth == nil {
		hw.logger.Warn("File health record not found", "file_path", filePath)
		return fmt.Errorf("file health record not found for file: %s", filePath)
	}

	// For direct manual checks, we want to update the health record immediately
	// rather than using the normal health check result handling which may delete records
	var errorMsg *string
	if event.Error != nil {
		errorText := event.Error.Error()
		errorMsg = &errorText
	}
	
	// Update the health record with the new status
	err = hw.healthRepo.UpdateFileHealth(filePath, event.Status, errorMsg, fileHealth.SourceNzbPath, nil)
	if err != nil {
		hw.logger.Error("Failed to update health record after direct check", "file_path", filePath, "error", err)
		return fmt.Errorf("failed to update health record: %w", err)
	}
	
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

// runHealthCheckCycle runs a single cycle of health checks
func (hw *HealthWorker) runHealthCheckCycle(ctx context.Context) error {
	now := time.Now()
	hw.updateStats(func(s *WorkerStats) {
		s.CurrentRunStartTime = &now
		s.CurrentRunFilesChecked = 0
	})

	// Get unhealthy files that need checking
	unhealthyFiles, err := hw.healthRepo.GetUnhealthyFiles(hw.config.BatchSize)
	if err != nil {
		return fmt.Errorf("failed to get unhealthy files: %w", err)
	}

	if len(unhealthyFiles) == 0 {
		hw.logger.Debug("No unhealthy files found, skipping health check cycle")
		hw.updateStats(func(s *WorkerStats) {
			s.CurrentRunStartTime = nil
			s.CurrentRunFilesChecked = 0
			s.TotalRunsCompleted++
			s.LastRunTime = &now
			nextRun := now.Add(hw.config.CheckInterval)
			s.NextRunTime = &nextRun
		})
		return nil
	}

	hw.logger.Info("Found unhealthy files to check", "count", len(unhealthyFiles))

	// Create a health checker for this batch
	checkerConfig := HealthCheckerConfig{
		MaxConcurrentJobs:     hw.config.MaxConcurrentJobs,
		BatchSize:             hw.config.BatchSize,
		MaxRetries:            hw.config.MaxRetries,
		MaxSegmentConnections: hw.config.MaxSegmentConnections,
		CheckAllSegments:      hw.config.CheckAllSegments,
	}

	checker := NewHealthChecker(hw.healthRepo, hw.metadataService, hw.poolManager, checkerConfig)
	
	// Process files with progress tracking
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, hw.config.MaxConcurrentJobs)
	
	for _, fileHealth := range unhealthyFiles {
		wg.Add(1)
		go func(fh *database.FileHealth) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Check the file
			checker.checkFileFromHealth(ctx, fh)
			
			// Update progress
			hw.updateStats(func(s *WorkerStats) {
				s.CurrentRunFilesChecked++
				s.TotalFilesChecked++
			})
		}(fileHealth)
	}

	wg.Wait()

	// Update final stats
	hw.updateStats(func(s *WorkerStats) {
		s.CurrentRunStartTime = nil
		s.CurrentRunFilesChecked = 0
		s.TotalRunsCompleted++
		s.LastRunTime = &now
		nextRun := now.Add(hw.config.CheckInterval)
		s.NextRunTime = &nextRun
	})

	hw.logger.Info("Health check cycle completed", 
		"files_checked", len(unhealthyFiles),
		"duration", time.Since(now))

	return nil
}

// updateStats safely updates worker statistics
func (hw *HealthWorker) updateStats(updateFunc func(*WorkerStats)) {
	hw.statsMu.Lock()
	defer hw.statsMu.Unlock()
	updateFunc(&hw.stats)
}

// UpdateConfig updates the worker configuration
func (hw *HealthWorker) UpdateConfig(config HealthWorkerConfig) error {
	hw.mu.Lock()
	defer hw.mu.Unlock()

	oldEnabled := hw.config.Enabled
	hw.config = config

	// If enabled status changed, we need to restart
	if oldEnabled != config.Enabled {
		if hw.running {
			hw.logger.Info("Health worker enabled status changed, restart required")
		}
	}

	hw.logger.Info("Health worker configuration updated",
		"enabled", config.Enabled,
		"check_interval", config.CheckInterval,
		"batch_size", config.BatchSize,
		"max_retries", config.MaxRetries)

	return nil
}