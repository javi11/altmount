package health

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
)

// EventType represents the type of health event
type EventType string

const (
	EventTypeFileRecovered EventType = "file_recovered"
	EventTypeFileCorrupted EventType = "file_corrupted"
	EventTypeCheckFailed   EventType = "check_failed"
)

// HealthEvent represents a health check event
type HealthEvent struct {
	Type       EventType
	FilePath   string
	Status     database.HealthStatus
	Error      error
	Timestamp  time.Time
	RetryCount int
	SourceNzb  *string
}

// EventHandler handles health events
type EventHandler func(event HealthEvent)

// HealthCheckerConfig holds configuration for the health checker
type HealthCheckerConfig struct {
	CheckInterval         time.Duration // How often to run health checks
	MaxConcurrentJobs     int           // Maximum concurrent file checks
	BatchSize             int           // How many files to check in each batch
	MaxRetries            int           // Maximum retries before marking as permanently corrupted
	MaxSegmentConnections int           // Maximum concurrent connections for segment checking
	CheckAllSegments      bool          // Whether to check all segments or just first one
	EventHandler          EventHandler  // Optional event handler for notifications
}

// HealthChecker manages file health monitoring and recovery
type HealthChecker struct {
	healthRepo      *database.HealthRepository
	metadataService *metadata.MetadataService
	poolManager     pool.Manager
	config          HealthCheckerConfig

	running  bool
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.RWMutex
}

// NewHealthChecker creates a new health checker service
func NewHealthChecker(
	healthRepo *database.HealthRepository,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	config HealthCheckerConfig,
) *HealthChecker {
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
	// CheckAllSegments defaults to false (check only first segment)

	return &HealthChecker{
		healthRepo:      healthRepo,
		metadataService: metadataService,
		poolManager:     poolManager,
		config:          config,
		stopChan:        make(chan struct{}),
	}
}

// Start begins the health checking service
func (hc *HealthChecker) Start(ctx context.Context) error {
	hc.mu.Lock()
	if hc.running {
		hc.mu.Unlock()
		return fmt.Errorf("health checker already running")
	}
	hc.running = true
	hc.mu.Unlock()

	slog.Info("Starting health checker", "interval", hc.config.CheckInterval)

	hc.wg.Add(1)
	go func() {
		defer hc.wg.Done()
		hc.run(ctx)
	}()

	return nil
}

// Stop gracefully stops the health checking service
func (hc *HealthChecker) Stop() error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if !hc.running {
		return fmt.Errorf("health checker not running")
	}

	slog.Info("Stopping health checker...")
	close(hc.stopChan)
	hc.running = false

	// Wait for all goroutines to finish
	hc.wg.Wait()

	slog.Info("Health checker stopped")
	return nil
}

// IsRunning returns whether the health checker is currently running
func (hc *HealthChecker) IsRunning() bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.running
}

// CheckFileHealth manually checks the health of a specific file
func (hc *HealthChecker) CheckFileHealth(ctx context.Context, filePath string) (*HealthEvent, error) {
	// Get file metadata
	fileMeta, err := hc.metadataService.ReadFileMetadata(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file metadata: %w", err)
	}
	if fileMeta == nil {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}

	// Perform the health check
	event := hc.checkSingleFile(ctx, filePath, fileMeta)
	return &event, nil
}

// run is the main health checking loop
func (hc *HealthChecker) run(ctx context.Context) {
	ticker := time.NewTicker(hc.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Health checker stopped by context")
			return
		case <-hc.stopChan:
			slog.Info("Health checker stopped by stop signal")
			return
		case <-ticker.C:
			if err := hc.runHealthCheckCycle(ctx); err != nil {
				slog.Error("Health check cycle failed", "error", err)
			}
		}
	}
}

// runHealthCheckCycle runs a single cycle of health checks
func (hc *HealthChecker) runHealthCheckCycle(ctx context.Context) error {
	// Get unhealthy files that need checking
	unhealthyFiles, err := hc.healthRepo.GetUnhealthyFiles(hc.config.BatchSize)
	if err != nil {
		return fmt.Errorf("failed to get unhealthy files: %w", err)
	}

	if len(unhealthyFiles) == 0 {
		slog.Info("No unhealthy files found, skipping health check cycle")
		return nil
	}

	slog.Info("Found unhealthy files to check", "count", len(unhealthyFiles))

	for _, fileHealth := range unhealthyFiles {
		err = hc.healthRepo.SetFileChecking(fileHealth.FilePath)
		if err != nil {
			return err
		}
	}

	// Create a semaphore to limit concurrent checks
	semaphore := make(chan struct{}, hc.config.MaxConcurrentJobs)
	var wg sync.WaitGroup

	for _, fileHealth := range unhealthyFiles {
		wg.Add(1)
		go func(fh *database.FileHealth) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Check the file
			hc.checkFileFromHealth(ctx, fh)
		}(fileHealth)
	}

	wg.Wait()
	return nil
}

// checkFileFromHealth checks a file based on its health record
func (hc *HealthChecker) checkFileFromHealth(ctx context.Context, fileHealth *database.FileHealth) {
	// Get current metadata
	fileMeta, err := hc.metadataService.ReadFileMetadata(fileHealth.FilePath)
	if err != nil {
		slog.Error("Failed to read metadata", "file_path", fileHealth.FilePath, "error", err)
		return
	}
	if fileMeta == nil {
		slog.Info("File metadata not found, cleaning up health record", "file_path", fileHealth.FilePath)
		// TODO: Clean up orphaned health record
		return
	}

	// Perform the health check
	event := hc.checkSingleFile(ctx, fileHealth.FilePath, fileMeta)

	// Handle the result
	hc.handleHealthCheckResult(event, fileHealth)
}

// checkSingleFile performs a health check on a single file
func (hc *HealthChecker) checkSingleFile(ctx context.Context, filePath string, fileMeta *metapb.FileMetadata) HealthEvent {
	event := HealthEvent{
		FilePath:  filePath,
		Timestamp: time.Now(),
		SourceNzb: &fileMeta.SourceNzbPath,
	}

	if len(fileMeta.SegmentData) == 0 {
		event.Type = EventTypeCheckFailed
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("no segment data available")
		return event
	}

	var segmentsToCheck []*metapb.SegmentData
	if hc.config.CheckAllSegments {
		// Check all segments
		segmentsToCheck = fileMeta.SegmentData
	} else {
		// Check only first segment (faster, default behavior)
		segmentsToCheck = []*metapb.SegmentData{fileMeta.SegmentData[0]}
	}

	// Check segments with configurable concurrency
	missingSegments, totalSegments, checkErr := hc.checkSegments(ctx, segmentsToCheck)

	if checkErr != nil {
		event.Type = EventTypeCheckFailed
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("failed to check segments: %w", checkErr)
		return event
	}

	// Determine file health based on missing segments
	if missingSegments == 0 {
		// All checked segments are available
		event.Type = EventTypeFileRecovered
		event.Status = database.HealthStatusHealthy
	} else if missingSegments < totalSegments {
		// Some segments missing
		event.Type = EventTypeFileCorrupted
		event.Status = database.HealthStatusPartial
		event.Error = fmt.Errorf("partial file: %d/%d segments missing", missingSegments, totalSegments)
	} else {
		// All segments missing
		event.Type = EventTypeFileCorrupted
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("corrupted file: %d/%d segments missing", missingSegments, totalSegments)
	}

	return event
}

// checkSegments checks multiple segments concurrently with connection limit
func (hc *HealthChecker) checkSegments(ctx context.Context, segments []*metapb.SegmentData) (missingCount, totalCount int, err error) {
	totalCount = len(segments)
	if totalCount == 0 {
		return 0, 0, nil
	}

	// Create semaphore to limit concurrent connections
	semaphore := make(chan struct{}, hc.config.MaxSegmentConnections)
	results := make(chan bool, totalCount)
	errors := make(chan error, totalCount)

	var wg sync.WaitGroup

	// Check all segments concurrently
	for _, segment := range segments {
		wg.Add(1)
		go func(seg *metapb.SegmentData) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				errors <- ctx.Err()
				return
			}

			// Check if segment exists
			available, checkErr := hc.checkSingleSegment(ctx, seg.Id)
			if checkErr != nil {
				errors <- checkErr
				return
			}

			results <- available
		}(segment)
	}

	// Wait for all checks to complete
	wg.Wait()
	close(results)
	close(errors)

	// Check for errors first
	select {
	case err := <-errors:
		return 0, totalCount, err
	default:
	}

	// Count missing segments
	missingCount = 0
	for available := range results {
		if !available {
			missingCount++
		}
	}

	return missingCount, totalCount, nil
}

// checkSingleSegment checks if a single segment exists
func (hc *HealthChecker) checkSingleSegment(ctx context.Context, segmentID string) (bool, error) {
	// Get current pool from manager
	usenetPool, err := hc.poolManager.GetPool()
	if err != nil {
		return false, fmt.Errorf("failed to get usenet pool: %w", err)
	}

	responseCode, err := usenetPool.Stat(ctx, segmentID, []string{})
	if err != nil {
		return false, fmt.Errorf("failed to check article %s: %w", segmentID, err)
	}

	// NNTP response codes: 223 = article exists, other codes indicate issues
	return responseCode == 223, nil
}

// handleHealthCheckResult handles the result of a health check
func (hc *HealthChecker) handleHealthCheckResult(event HealthEvent, fileHealth *database.FileHealth) {
	switch event.Type {
	case EventTypeFileRecovered:
		// File is now healthy - update metadata and delete from health database
		slog.Info("File recovered", "file_path", event.FilePath)

		// Update metadata status
		if err := hc.metadataService.UpdateFileStatus(event.FilePath, metapb.FileStatus_FILE_STATUS_HEALTHY); err != nil {
			slog.Error("Failed to update metadata status", "file_path", event.FilePath, "error", err)
		}

		// Delete health record since file is now healthy
		if err := hc.healthRepo.DeleteHealthRecord(event.FilePath); err != nil {
			slog.Error("Failed to delete health record for recovered file", "file_path", event.FilePath, "error", err)
		} else {
			slog.Info("Removed health record for recovered file", "file_path", event.FilePath)
		}

	case EventTypeFileCorrupted:
		// File is still corrupted - increment retry count or mark as permanently corrupted
		slog.Warn("File still corrupted", "file_path", event.FilePath, "retry_count", fileHealth.RetryCount, "max_retries", fileHealth.MaxRetries)

		errorMsg := event.Error.Error()

		if fileHealth.RetryCount >= fileHealth.MaxRetries-1 {
			// Max retries reached - mark as permanently corrupted
			if err := hc.healthRepo.MarkAsCorrupted(event.FilePath, &errorMsg); err != nil {
				slog.Error("Failed to mark file as corrupted", "error", err)
			}
			slog.Error("File permanently marked as corrupted", "file_path", event.FilePath)
		} else {
			// Increment retry count
			if err := hc.healthRepo.IncrementRetryCount(event.FilePath, &errorMsg); err != nil {
				slog.Error("Failed to increment retry count", "file_path", event.FilePath, "error", err)
			}
		}

	case EventTypeCheckFailed:
		// Health check failed - increment retry count
		slog.Error("Health check failed", "file_path", event.FilePath, "error", event.Error)

		errorMsg := event.Error.Error()
		if err := hc.healthRepo.IncrementRetryCount(event.FilePath, &errorMsg); err != nil {
			slog.Error("Failed to increment retry count", "file_path", event.FilePath, "error", err)
		}
	}

	// Emit event if handler is configured
	if hc.config.EventHandler != nil {
		event.RetryCount = fileHealth.RetryCount
		hc.config.EventHandler(event)
	}
}

// GetHealthStats returns current health statistics
func (hc *HealthChecker) GetHealthStats() (map[database.HealthStatus]int, error) {
	return hc.healthRepo.GetHealthStats()
}
