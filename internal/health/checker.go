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
	EventTypeFileHealthy   EventType = "file_healthy"
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
	Details    *string
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

func (hc *HealthChecker) getMaxConnectionsForHealthChecks() int {
	connections := hc.configGetter().Health.MaxConnectionsForHealthChecks
	if connections <= 0 {
		return 5 // Default
	}
	return connections
}

func (hc *HealthChecker) getSegmentSamplePercentage() int {
	percentage := hc.configGetter().Health.SegmentSamplePercentage
	if percentage < 1 || percentage > 100 {
		return 5 // Default: 5%
	}
	return percentage
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
		_ = hc.healthRepo.DeleteHealthRecord(ctx, filePath)

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

	slog.InfoContext(ctx, "Checking segment availability", "file_path", filePath, "total_segments", len(fileMeta.SegmentData), "sample_percentage", hc.getSegmentSamplePercentage())

	// Validate segment availability using detailed validation logic
	result, err := usenet.ValidateSegmentAvailabilityDetailed(
		ctx,
		fileMeta.SegmentData,
		hc.poolManager,
		hc.getMaxConnectionsForHealthChecks(),
		hc.getSegmentSamplePercentage(),
		nil, // No progress callback for health checks
	)

	if err != nil {
		event.Type = EventTypeCheckFailed
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("failed to validate segments: %w", err)
		return event
	}

	if result.MissingCount > 0 {
		event.Type = EventTypeFileCorrupted
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("file corrupted: missing %d/%d checked segments", result.MissingCount, result.TotalChecked)

		// Create detailed JSON report
		details := fmt.Sprintf(`{"missing_count": %d, "total_checked": %d, "missing_ids": %q}`,
			result.MissingCount, result.TotalChecked, result.MissingIDs)
		event.Details = &details

		return event
	}

	// All checked segments are available - record will be deleted
	event.Type = EventTypeFileHealthy
	// Status not needed as the record will be deleted from database

	return event
}

// NotifyRcloneVFS notifies rclone VFS about a file status change (async, non-blocking)
func (hc *HealthChecker) notifyRcloneVFS(filePath string, event HealthEvent) {
	if hc.rcloneClient == nil {
		return // No rclone client configured
	}

	// Only notify on significant status changes (healthy <-> corrupted)
	switch event.Type {
	case EventTypeFileHealthy, EventTypeFileCorrupted:
		// Continue with notification
	default:
		return // No notification needed for other event types
	}

	// Start async notification
	go func() {
		// Extract directory path from file path for VFS refresh
		virtualDir := filepath.Dir(filePath)

		// Use background context with timeout for VFS notification
		// Increased timeout to 60 seconds as vfs/refresh can be slow
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		cfg := hc.configGetter()
		vfsName := cfg.RClone.VFSName
		if vfsName == "" {
			vfsName = config.MountProvider
		}

		// Refresh cache asynchronously to avoid blocking health checks
		err := hc.rcloneClient.RefreshDir(ctx, vfsName, []string{virtualDir})
		if err != nil {
			slog.ErrorContext(ctx, "Failed to notify rclone VFS about file status change", "file", filePath, "event", event.Type, "err", err)
		}
	}()
}

// GetHealthStats returns current health statistics
func (hc *HealthChecker) GetHealthStats(ctx context.Context) (map[database.HealthStatus]int, error) {
	return hc.healthRepo.GetHealthStats(ctx)
}
