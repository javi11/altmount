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

// CheckOptions defines options for health checking
type CheckOptions struct {
	ForceFullCheck bool
}

// HealthChecker manages file health checking logic
type HealthChecker struct {
	healthRepo      *database.HealthRepository
	metadataService *metadata.MetadataService
	poolManager     pool.Manager
	configGetter    config.ConfigGetter
	rcloneClient    rclonecli.RcloneRcClient // Optional rclone client for VFS notifications
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(
	healthRepo *database.HealthRepository,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	configGetter config.ConfigGetter,
	rcloneClient rclonecli.RcloneRcClient,
) *HealthChecker {
	return &HealthChecker{
		healthRepo:      healthRepo,
		metadataService: metadataService,
		poolManager:     poolManager,
		configGetter:    configGetter,
		rcloneClient:    rcloneClient,
	}
}

// CheckFile checks the health of a specific file
func (hc *HealthChecker) CheckFile(ctx context.Context, filePath string, opts ...CheckOptions) HealthEvent {
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
		if err := hc.healthRepo.DeleteHealthRecord(ctx, filePath); err != nil {
			slog.ErrorContext(ctx, "Failed to delete health record for removed file", "file_path", filePath, "error", err)
		}

		return HealthEvent{
			Type:      EventTypeFileRemoved,
			FilePath:  filePath,
			Status:    database.HealthStatusCorrupted,
			Error:     fmt.Errorf("file not found: %s", filePath),
			Timestamp: time.Now(),
		}
	}

	// Perform the health check
	return hc.checkSingleFile(ctx, filePath, fileMeta, opts...)
}

// checkSingleFile performs a health check on a single file
func (hc *HealthChecker) checkSingleFile(ctx context.Context, filePath string, fileMeta *metapb.FileMetadata, opts ...CheckOptions) HealthEvent {
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

	cfg := hc.configGetter()
	samplePercentage := cfg.GetSegmentSamplePercentage()
	verifyData := cfg.GetVerifyData()

	if cfg.GetCheckAllSegments() {
		samplePercentage = 100
		verifyData = true // Always verify data when doing a full deep check
	}

	// Override sample percentage if forced full check is requested
	if len(opts) > 0 && opts[0].ForceFullCheck {
		samplePercentage = 100
		verifyData = true
		slog.InfoContext(ctx, "Forcing full health check (100% sampling + data verification)", "file_path", filePath)
	}

	slog.InfoContext(ctx, "Checking segment availability",
		"file_path", filePath,
		"total_segments", len(fileMeta.SegmentData),
		"sample_percentage", samplePercentage,
		"verify_data", verifyData)

	// 1. Metadata integrity check - Verify the entire file map is complete
	loader := &metadataSegmentLoader{segments: fileMeta.SegmentData}
	if err := usenet.CheckMetadataIntegrity(fileMeta.FileSize, loader); err != nil {
		event.Type = EventTypeFileCorrupted
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("metadata corruption: %w", err)
		details := fmt.Sprintf(`{"error": "metadata_gap", "message": %q}`, err.Error())
		event.Details = &details
		return event
	}

	// 2. Network availability check - Validate segment availability using detailed validation logic
	result, err := usenet.ValidateSegmentAvailabilityDetailed(
		ctx,
		fileMeta.SegmentData,
		hc.poolManager,
		cfg.GetMaxConnectionsForHealthChecks(),
		samplePercentage,
		nil, // No progress callback for health checks
		30*time.Second,
		verifyData,
	)

	if err != nil {
		event.Type = EventTypeCheckFailed
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("failed to validate segments: %w", err)
		return event
	}

	if result.MissingCount > 0 {
		// Calculate missing percentage
		missingPercentage := (float64(result.MissingCount) / float64(result.TotalChecked)) * 100

		// Check if missing percentage is within acceptable threshold
		acceptableThreshold := cfg.Health.AcceptableMissingSegmentsPercentage
		if missingPercentage <= acceptableThreshold {
			slog.InfoContext(ctx, "File has missing segments but within acceptable threshold",
				"file_path", filePath,
				"missing_count", result.MissingCount,
				"total_checked", result.TotalChecked,
				"missing_percentage", fmt.Sprintf("%.2f%%", missingPercentage),
				"threshold", fmt.Sprintf("%.2f%%", acceptableThreshold))

			// Treat as healthy
			event.Type = EventTypeFileHealthy
			return event
		}

		event.Type = EventTypeFileCorrupted
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("file corrupted: missing %d/%d checked segments (%.2f%%)",
			result.MissingCount, result.TotalChecked, missingPercentage)

		// Create detailed JSON report
		details := fmt.Sprintf(`{"missing_count": %d, "total_checked": %d, "missing_percentage": %.2f, "missing_ids": %q}`,
			result.MissingCount, result.TotalChecked, missingPercentage, result.MissingIDs)
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

	// Only notify for rclone-based mounts; FUSE and none don't use rclone VFS
	cfg := hc.configGetter()
	switch cfg.MountType {
	case config.MountTypeRClone, config.MountTypeRCloneExternal:
		// continue
	default:
		return
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

type metadataSegmentLoader struct {
	segments []*metapb.SegmentData
}

func (l *metadataSegmentLoader) GetSegment(index int) (usenet.Segment, []string, bool) {
	if index < 0 || index >= len(l.segments) {
		return usenet.Segment{}, nil, false
	}

	s := l.segments[index]
	return usenet.Segment{
		Id:    s.Id,
		Start: s.StartOffset,
		End:   s.EndOffset,
		Size:  s.SegmentSize,
	}, []string{}, true
}
