package health

import (
	"context"
	"fmt"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	concpool "github.com/sourcegraph/conc/pool"
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

// HealthConfig holds unified configuration for health checking
type HealthConfig struct {
	// Worker settings
	Enabled           bool          // Whether health worker is enabled
	CheckInterval     time.Duration // How often to run health checks
	MaxConcurrentJobs int           // How many files to check in each batch

	// Health check settings
	MaxRetries            int          // Maximum retries before marking as permanently corrupted
	MaxSegmentConnections int          // Maximum concurrent connections for segment checking
	CheckAllSegments      bool         // Whether to check all segments or just first one
	EventHandler          EventHandler // Optional event handler for notifications
}

// HealthChecker manages file health checking logic
type HealthChecker struct {
	healthRepo      *database.HealthRepository
	metadataService *metadata.MetadataService
	poolManager     pool.Manager
	config          HealthConfig
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(
	healthRepo *database.HealthRepository,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	config HealthConfig,
) *HealthChecker {
	// Set defaults if not provided
	if config.MaxConcurrentJobs == 0 {
		config.MaxConcurrentJobs = 1
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
	}
}

// CheckFile checks the health of a specific file
func (hc *HealthChecker) CheckFile(ctx context.Context, filePath string) HealthEvent {
	// Get file metadata
	fileMeta, err := hc.metadataService.ReadFileMetadata(filePath)
	if err != nil {
		return HealthEvent{
			Type:      EventTypeCheckFailed,
			FilePath:  filePath,
			Status:    database.HealthStatusCorrupted,
			Error:     fmt.Errorf("failed to read file metadata: %w", err),
			Timestamp: time.Now(),
		}
	}
	if fileMeta == nil {
		return HealthEvent{
			Type:      EventTypeCheckFailed,
			FilePath:  filePath,
			Status:    database.HealthStatusCorrupted,
			Error:     fmt.Errorf("file not found: %s", filePath),
			Timestamp: time.Now(),
		}
	}

	// Perform the health check
	return hc.checkSingleFile(ctx, filePath, fileMeta)
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

	// Create pool with concurrency limit and context cancellation
	p := concpool.NewWithResults[bool]().
		WithMaxGoroutines(hc.config.MaxSegmentConnections).
		WithContext(ctx)

	// Check all segments concurrently using pool
	for _, segment := range segments {
		p.Go(func(ctx context.Context) (bool, error) {
			return hc.checkSingleSegment(ctx, segment.Id)
		})
	}

	// Wait for all checks to complete and collect results
	results, err := p.Wait()
	if err != nil {
		return 0, totalCount, err
	}

	// Count missing segments
	missingCount = 0
	for _, available := range results {
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

// GetHealthStats returns current health statistics
func (hc *HealthChecker) GetHealthStats() (map[database.HealthStatus]int, error) {
	return hc.healthRepo.GetHealthStats()
}
