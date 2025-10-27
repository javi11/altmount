package health

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/altmount/pkg/rclonecli"
)

// EventType represents the type of health event
type EventType string

const (
	EventTypeFileRecovered EventType = "file_recovered"
	EventTypeFileCorrupted EventType = "file_corrupted"
	EventTypeCheckFailed   EventType = "check_failed"
	EventTypeFileRemoved   EventType = "file_removed"
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
	configGetter    config.ConfigGetter
	rcloneClient    rclonecli.RcloneRcClient // Optional rclone client for VFS notifications
	eventHandler    EventHandler             // Optional event handler for notifications
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(
	healthRepo *database.HealthRepository,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	configGetter config.ConfigGetter,
	rcloneClient rclonecli.RcloneRcClient,
	eventHandler EventHandler,
) *HealthChecker {
	return &HealthChecker{
		healthRepo:      healthRepo,
		metadataService: metadataService,
		poolManager:     poolManager,
		configGetter:    configGetter,
		rcloneClient:    rcloneClient,
		eventHandler:    eventHandler,
	}
}

func (hc *HealthChecker) getMaxSegmentConnections() int {
	connections := hc.configGetter().Health.MaxSegmentConnections
	if connections <= 0 {
		return 5 // Default
	}
	return connections
}

func (hc *HealthChecker) getCheckAllSegments() bool {
	return hc.configGetter().Health.CheckAllSegments
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
		// File not found - remove from health database
		_ = hc.healthRepo.DeleteHealthRecord(filePath)

		return HealthEvent{
			Type:      EventTypeFileRemoved,
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

	slog.Info("Checking segment availability", "file_path", filePath, "total_segments", len(fileMeta.SegmentData), "full_validation", hc.getCheckAllSegments())

	// Validate segment availability using shared validation logic
	checkErr := usenet.ValidateSegmentAvailability(
		ctx,
		fileMeta.SegmentData,
		hc.poolManager,
		hc.getMaxSegmentConnections(),
		hc.getCheckAllSegments(),
	)

	if checkErr != nil {
		event.Type = EventTypeCheckFailed
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("corrupted file some segments are missing")
		return event
	}

	// All checked segments are available
	event.Type = EventTypeFileRecovered
	event.Status = database.HealthStatusHealthy

	return event
}

// NotifyRcloneVFS notifies rclone VFS about a file status change (async, non-blocking)
func (hc *HealthChecker) notifyRcloneVFS(filePath string, event HealthEvent) {
	if hc.rcloneClient == nil {
		return // No rclone client configured
	}

	// Only notify on significant status changes (healthy <-> corrupted)
	switch event.Type {
	case EventTypeFileRecovered, EventTypeFileCorrupted:
		// Continue with notification
	default:
		return // No notification needed for other event types
	}

	// Start async notification
	go func() {
		// Extract directory path from file path for VFS refresh
		virtualDir := filepath.Dir(filePath)

		// Use background context with timeout for VFS notification
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Refresh cache asynchronously to avoid blocking health checks
		err := hc.rcloneClient.RefreshDir(ctx, config.MountProvider, []string{virtualDir}) // Use RefreshDir with empty provider
		if err != nil {
			slog.Error("Failed to notify rclone VFS about file status change", "file", filePath, "event", event.Type, "err", err)
		}
	}()
}

// GetHealthStats returns current health statistics
func (hc *HealthChecker) GetHealthStats() (map[database.HealthStatus]int, error) {
	return hc.healthRepo.GetHealthStats()
}
