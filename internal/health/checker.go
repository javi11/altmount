package health

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/contentverify"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/holes"
	"github.com/javi11/altmount/internal/importer/parser/fileinfo"
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
	// Classification is the playback-impact verdict for video files with
	// missing segments (nil when not applicable).
	Classification *holes.Impact
}

// CheckOptions defines options for health checking
type CheckOptions struct {
	ForceFullCheck bool
	// CurrentStatus is the file's HealthStatus before this check started,
	// used to gate content verification to first-time (Pending) checks.
	// Set by the caller (HealthWorker), which already has the row loaded.
	CurrentStatus database.HealthStatus
	// VerifyContentOverride, when non-nil, forces (true) or disables
	// (false) content verification for this check regardless of the
	// file's current status or the configured default. Used by the
	// manual recheck API to let a user force a re-probe.
	VerifyContentOverride *bool
}

// HealthChecker manages file health checking logic
type HealthChecker struct {
	healthRepo      *database.HealthRepository
	metadataService *metadata.MetadataService
	poolManager     pool.Manager
	configGetter    config.ConfigGetter
	rcloneClient    rclonecli.RcloneRcClient // Optional rclone client for VFS notifications
	contentVerifyFS contentverify.Opener     // Optional: real NzbFilesystem for content probing
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(
	healthRepo *database.HealthRepository,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	configGetter config.ConfigGetter,
	rcloneClient rclonecli.RcloneRcClient,
	contentVerifyFS contentverify.Opener,
) *HealthChecker {
	return &HealthChecker{
		healthRepo:      healthRepo,
		metadataService: metadataService,
		poolManager:     poolManager,
		configGetter:    configGetter,
		rcloneClient:    rcloneClient,
		contentVerifyFS: contentVerifyFS,
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
	encryption    metapb.Encryption
	// knownHoles is the file's persisted hole map (segments confirmed
	// missing by earlier sweeps or playback padding).
	knownHoles []*metapb.HoleRun
	// hasNestedOrRemuxedSources marks files whose bytes are not a plain
	// segment concatenation (nested RAR sources, BD clip remux) — those are
	// never zero-filled, so hole classification does not apply.
	hasNestedOrRemuxedSources bool
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
	// totalSegments is the full (unsampled) segment count, kept as a scalar so
	// it survives past preparation for error reporting without holding onto
	// the segment slice itself during the network sweep.
	totalSegments int
	// currentStatus is the file's HealthStatus before this check started,
	// used to gate content verification to first-time (Pending) checks
	// unless verifyContentOverride forces it either way.
	currentStatus         database.HealthStatus
	verifyContentOverride *bool
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
	if len(opts) > 0 {
		prep.currentStatus = opts[0].CurrentStatus
		prep.verifyContentOverride = opts[0].VerifyContentOverride
	}

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
		encryption:    fileMeta.Encryption,
		knownHoles:    fileMeta.KnownHoles,
		hasNestedOrRemuxedSources: len(fileMeta.NestedSources) > 0 ||
			len(fileMeta.SharedOuterSources) > 0 ||
			len(fileMeta.ClipBoundaries) > 0,
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
		"sample_percentage", samplePercentage,
	)

	prep.totalSegments = len(input.segments)

	// 1. Metadata integrity check - Verify the entire file map is complete
	loader := &metadataSegmentLoader{segments: input.segments}
	if err := usenet.CheckMetadataIntegrity(input.fileSize, loader); err != nil {
		event := baseResultEvent(filePath, input.sourceNzbPath)
		event.Type = EventTypeFileCorrupted
		event.Status = database.HealthStatusCorrupted
		event.Error = fmt.Errorf("metadata corruption: %w", err)
		details := database.HealthErrorDetails{ErrorType: "metadata_gap", Message: err.Error()}
		event.Details = details.Marshal()
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
// It is a method (not a free function) because a missing-segment outcome
// classifies playback impact via the hole model, which re-reads metadata
// through hc.metadataService.
func (hc *HealthChecker) judgeValidation(ctx context.Context, prep preparedCheck, result usenet.ValidationResult, valErr error) HealthEvent {
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
		event.Classification = hc.classifyHoles(ctx, prep.filePath, result)
		details := database.HealthErrorDetails{
			ErrorType:       "missing_segments",
			MissingArticles: result.MissingCount,
			TotalArticles:   prep.totalSegments,
			Sampled:         result.TotalChecked,
			PlaybackImpact:  event.Classification,
		}
		event.Details = details.Marshal()
		return event
	}

	if hc.shouldVerifyContent(prep) {
		if verified := hc.judgeContentVerification(ctx, prep); verified != nil {
			return *verified
		}
	}

	// All checked segments are available - record will be deleted.
	// Persisted known holes (from playback padding) survive on purpose: a
	// clean STAT sample never overrides observed misses.
	event.Type = EventTypeFileHealthy
	// Status not needed as the record will be deleted from database

	return event
}

// shouldVerifyContent decides whether judgeValidation should run a content
// probe for this file: explicit override wins; otherwise only a first-time
// (Pending) check is probed, so repeated repair-recheck cycles on an
// already-flagged file don't re-probe on every pass.
func (hc *HealthChecker) shouldVerifyContent(prep preparedCheck) bool {
	if prep.verifyContentOverride != nil {
		return *prep.verifyContentOverride
	}
	if hc.contentVerifyFS == nil || !hc.configGetter().GetHealthVerifyContent() {
		return false
	}
	return prep.currentStatus == database.HealthStatusPending
}

// judgeContentVerification probes prep.filePath's content signature and, if
// the result is definitive, returns a Corrupted event. A nil return means
// either verification is not eligible for this file (not a verifiable media
// type), passed, or failed only transiently — in all three cases the caller
// proceeds to the normal healthy branch, since a transient probe error must
// never mark a file corrupted.
func (hc *HealthChecker) judgeContentVerification(ctx context.Context, prep preparedCheck) *HealthEvent {
	if !fileinfo.IsVerifiableMediaFile(prep.filePath) {
		return nil
	}

	cfg := hc.configGetter()
	result := contentverify.Probe(ctx, hc.contentVerifyFS, prep.filePath, cfg.GetHealthVerifyContentTimeout())

	var errType, message string
	switch result.Result {
	case contentverify.ContentValid, contentverify.ContentProbeError:
		return nil
	case contentverify.ContentInvalid:
		errType = "content_invalid"
		message = "no recognized media container signature was found in the file's header"
	case contentverify.ContentSegmentMissing:
		errType = "content_segment_missing"
		message = "the article needed to read the file's header is missing from your Usenet provider"
	default:
		return nil
	}

	event := baseResultEvent(prep.filePath, prep.sourceNzbPath)
	event.Type = EventTypeFileCorrupted
	event.Status = database.HealthStatusCorrupted
	event.Error = fmt.Errorf("content verification failed: %s", message)
	details := database.HealthErrorDetails{ErrorType: errType, Message: message}
	event.Details = details.Marshal()
	return &event
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
	return hc.judgeValidation(ctx, prep, result, err)
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
// CheckFilesBatch checks the health of multiple files in a single sweep.
// statuses, when non-nil, must be index-aligned with filePaths and carries
// each file's current HealthStatus so content verification (when enabled)
// only probes first-time (Pending) checks; a nil or short statuses slice
// simply leaves those files' currentStatus at its zero value, disabling
// content verification for them.
func (hc *HealthChecker) CheckFilesBatch(ctx context.Context, filePaths []string, statuses []database.HealthStatus, opts ...CheckOptions) []HealthEvent {
	if len(filePaths) == 0 {
		return nil
	}

	preps := make([]preparedCheck, len(filePaths))
	pl := concpool.New().WithMaxGoroutines(min(len(filePaths), prepareConcurrency))
	for i, filePath := range filePaths {
		pl.Go(func() {
			preps[i] = hc.prepareCheck(ctx, filePath, opts...)
			if i < len(statuses) {
				preps[i].currentStatus = statuses[i]
			}
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
		events[i] = hc.judgeValidation(ctx, preps[i], result, valErr)
	}
	return events
}

// NotifyRcloneVFS notifies rclone VFS to forget and refresh the directory containing filePath (async, non-blocking).
func (hc *HealthChecker) NotifyRcloneVFS(filePath string) {
	if hc == nil || hc.rcloneClient == nil {
		return
	}
	cfg := hc.configGetter()
	switch cfg.MountType {
	case config.MountTypeRClone, config.MountTypeRCloneExternal:
		// continue
	default:
		return
	}

	virtualDir := filepath.Dir(filePath)
	hc.NotifyRcloneVFSDirs([]string{virtualDir})
}

// NotifyRcloneVFSDirs notifies rclone VFS to forget and refresh the specified directories (async, non-blocking).
func (hc *HealthChecker) NotifyRcloneVFSDirs(dirs []string) {
	if hc == nil || hc.rcloneClient == nil || len(dirs) == 0 {
		return
	}
	cfg := hc.configGetter()
	switch cfg.MountType {
	case config.MountTypeRClone, config.MountTypeRCloneExternal:
		// continue
	default:
		return
	}

	// Filter and deduplicate directories
	uniqueDirs := make([]string, 0, len(dirs))
	seen := make(map[string]bool)
	for _, d := range dirs {
		if d == "" {
			d = "/"
		}
		if !seen[d] {
			seen[d] = true
			uniqueDirs = append(uniqueDirs, d)
		}
	}
	if len(uniqueDirs) == 0 {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		vfsName := cfg.RClone.VFSName
		if vfsName == "" {
			vfsName = config.MountProvider
		}

		err := hc.rcloneClient.RefreshDir(ctx, vfsName, uniqueDirs)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to notify rclone VFS to forget/refresh directories", "dirs", uniqueDirs, "err", err)
		}
	}()
}

// notifyRcloneVFS notifies rclone VFS about a file status change (async, non-blocking)
func (hc *HealthChecker) notifyRcloneVFS(filePath string, event HealthEvent) {
	switch event.Type {
	case EventTypeFileHealthy, EventTypeFileCorrupted:
		hc.NotifyRcloneVFS(filePath)
	default:
		return
	}
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
