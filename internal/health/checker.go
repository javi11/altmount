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
	concpool "github.com/sourcegraph/conc/pool"
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
	Type      EventType
	FilePath  string
	Status    database.HealthStatus
	Error     error
	Details   *string
	Timestamp time.Time
	SourceNzb *string
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

// healthCheckInput holds the fields extracted from FileMetadata that the
// health check path actually needs. Passing this lean struct — instead of the
// full *metapb.FileMetadata — lets the proto wrapper be GC'd while the NNTP
// stat sweep performs long-running round-trips. Only SegmentData must remain
// referenced until sampling copies the message IDs; everything else is scalar.
type healthCheckInput struct {
	fileSize      int64
	sourceNzbPath string
	segments      []*metapb.SegmentData
}

// preparedCheck is the outcome of the per-file preparation stage shared by the
// single-file and batch check paths: either an early terminal event (metadata
// missing/corrupt, no segments) or the sampled segment IDs to Stat. Only the
// ID strings survive past preparation, so the proto segment slice is
// collectible before the network sweep begins.
type preparedCheck struct {
	filePath      string
	sourceNzbPath string
	sampledIDs    []string
	earlyEvent    *HealthEvent
}

// baseResultEvent builds the shared HealthEvent skeleton. SourceNzbPath is
// copied to an independent string so the event does not retain a pointer into
// the original proto (which would keep the whole message alive through any
// downstream consumer of the event).
func baseResultEvent(filePath, sourceNzbPath string) HealthEvent {
	sourceNzb := sourceNzbPath
	return HealthEvent{
		FilePath:  filePath,
		Timestamp: time.Now(),
		SourceNzb: &sourceNzb,
	}
}

// prepareCheck runs the local (non-network) stages of a health check: metadata
// read, integrity verification, and segment sampling. It returns either an
// early terminal event or the sampled segment IDs for the network sweep.
func (hc *HealthChecker) prepareCheck(ctx context.Context, filePath string, opts ...CheckOptions) preparedCheck {
	prep := preparedCheck{filePath: filePath}

	// Get file metadata
	fileMeta, err := hc.metadataService.ReadFileMetadata(filePath)
	if err != nil {
		event := HealthEvent{
			Type:      EventTypeFileCorrupted,
			FilePath:  filePath,
			Status:    database.HealthStatusCorrupted,
			Error:     fmt.Errorf("failed to read file metadata: %w", err),
			Timestamp: time.Now(),
		}
		details := fmt.Sprintf(`{"error": "metadata_read_failed", "message": %q}`, err.Error())
		event.Details = &details
		prep.earlyEvent = &event
		return prep
	}
	if fileMeta == nil {
		// File not found - remove from health database
		if err := hc.healthRepo.DeleteHealthRecord(ctx, filePath); err != nil {
			slog.ErrorContext(ctx, "Failed to delete health record for removed file", "file_path", filePath, "error", err)
		}

		event := HealthEvent{
			Type:      EventTypeFileRemoved,
			FilePath:  filePath,
			Status:    database.HealthStatusCorrupted,
			Error:     fmt.Errorf("file not found: %s", filePath),
			Timestamp: time.Now(),
		}
		prep.earlyEvent = &event
		return prep
	}

	// Extract only the fields needed for validation. The local fileMeta pointer
	// then falls out of scope and becomes eligible for GC — its proto wrapper
	// (MessageState, unknownFields, sizeCache, Par2Files, NestedSources, etc.)
	// is freed before NNTP stat round-trips begin.
	input := healthCheckInput{
		fileSize:      fileMeta.FileSize,
		sourceNzbPath: fileMeta.SourceNzbPath,
		segments:      fileMeta.SegmentData,
	}
	fileMeta = nil //nolint:ineffassign // explicit drop so the proto can be collected

	prep.sourceNzbPath = input.sourceNzbPath

	if len(input.segments) == 0 {
		event := baseResultEvent(filePath, input.sourceNzbPath)
		event.Type = EventTypeCheckFailed
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("no segment data available")
		prep.earlyEvent = &event
		return prep
	}

	cfg := hc.configGetter()
	samplePercentage := cfg.GetSegmentSamplePercentage()

	if cfg.GetCheckAllSegments() {
		samplePercentage = 100
	}

	// Override sample percentage if forced full check is requested
	if len(opts) > 0 && opts[0].ForceFullCheck {
		samplePercentage = 100
		slog.InfoContext(ctx, "Forcing full health check (100% sampling)", "file_path", filePath)
	}

	slog.InfoContext(ctx, "Checking segment availability",
		"file_path", filePath,
		"total_segments", len(input.segments),
		"sample_percentage", samplePercentage)

	// 1. Metadata integrity check - Verify the entire file map is complete
	loader := &metadataSegmentLoader{segments: input.segments}
	if err := usenet.CheckMetadataIntegrity(input.fileSize, loader); err != nil {
		event := baseResultEvent(filePath, input.sourceNzbPath)
		event.Type = EventTypeFileCorrupted
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("metadata corruption: %w", err)
		details := fmt.Sprintf(`{"error": "metadata_gap", "message": %q}`, err.Error())
		event.Details = &details
		prep.earlyEvent = &event
		return prep
	}

	// Sample and copy the message IDs so the proto segment slice becomes
	// collectible before the network sweep begins.
	selected := usenet.SelectSegmentsForValidation(input.segments, samplePercentage)
	prep.sampledIDs = make([]string, len(selected))
	for i, seg := range selected {
		prep.sampledIDs[i] = seg.Id
	}

	return prep
}

// judgeValidation turns a prepared check's segment-sweep outcome into the
// terminal HealthEvent, mirroring the pre-batch per-file semantics exactly.
func judgeValidation(prep preparedCheck, result usenet.ValidationResult, valErr error) HealthEvent {
	event := baseResultEvent(prep.filePath, prep.sourceNzbPath)

	if valErr != nil {
		event.Type = EventTypeCheckFailed
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("failed to validate segments: %w", valErr)
		return event
	}

	if result.MissingCount > 0 {
		event.Type = EventTypeFileCorrupted
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("%d of %d checked segments are missing from your Usenet provider",
			result.MissingCount, result.TotalChecked)
		return event
	}

	// All checked segments are available - record will be deleted
	event.Type = EventTypeFileHealthy
	// Status not needed as the record will be deleted from database

	return event
}

// CheckFile checks the health of a specific file
func (hc *HealthChecker) CheckFile(ctx context.Context, filePath string, opts ...CheckOptions) HealthEvent {
	prep := hc.prepareCheck(ctx, filePath, opts...)
	if prep.earlyEvent != nil {
		return *prep.earlyEvent
	}

	cfg := hc.configGetter()
	results, err := usenet.ValidateSegmentAvailabilityBatch(
		ctx,
		[][]string{prep.sampledIDs},
		hc.poolManager,
		cfg.GetMaxConnectionsForHealthChecks(),
		cfg.GetHealthReadTimeout(),
	)

	var result usenet.ValidationResult
	if err == nil {
		result = results[0]
	}
	return judgeValidation(prep, result, err)
}

// prepareConcurrency bounds the parallel metadata-read phase of a batch check.
// Preparation is local disk I/O, so a small constant keeps seek pressure sane
// regardless of batch size.
const prepareConcurrency = 8

// CheckFilesBatch checks many files in one cycle: per-file preparation runs in
// a small parallel pool, then every prepared file's sampled segments are
// verified in a single cross-file StatMany sweep, and each file receives its
// own HealthEvent (index-aligned with filePaths). A sweep infrastructure
// failure (pool unavailable) yields a CheckFailed event for every file that
// reached the network stage; per-file early events are unaffected.
func (hc *HealthChecker) CheckFilesBatch(ctx context.Context, filePaths []string, opts ...CheckOptions) []HealthEvent {
	if len(filePaths) == 0 {
		return nil
	}

	preps := make([]preparedCheck, len(filePaths))
	pl := concpool.New().WithMaxGoroutines(min(len(filePaths), prepareConcurrency))
	for i, filePath := range filePaths {
		pl.Go(func() {
			preps[i] = hc.prepareCheck(ctx, filePath, opts...)
		})
	}
	pl.Wait()

	perFileIDs := make([][]string, len(preps))
	for i := range preps {
		if preps[i].earlyEvent == nil {
			perFileIDs[i] = preps[i].sampledIDs
		}
	}

	cfg := hc.configGetter()
	results, valErr := usenet.ValidateSegmentAvailabilityBatch(
		ctx,
		perFileIDs,
		hc.poolManager,
		cfg.GetMaxConnectionsForHealthChecks(),
		cfg.GetHealthReadTimeout(),
	)

	events := make([]HealthEvent, len(preps))
	for i := range preps {
		if preps[i].earlyEvent != nil {
			events[i] = *preps[i].earlyEvent
			continue
		}
		var result usenet.ValidationResult
		if valErr == nil {
			result = results[i]
		}
		events[i] = judgeValidation(preps[i], result, valErr)
	}
	return events
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
