package nzbfilesystem

import (
	"context"
	"log/slog"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/holes"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/usenet"
)

// holeEligible reports whether this file's missing segments may be
// zero-filled: plain (unencrypted, non-nested, non-remuxed) video only.
// Padding anything else would silently corrupt copies/imports served
// through the mount.
func (mvf *MetadataVirtualFile) holeEligible() bool {
	return holes.EligibleFile(mvf.name) &&
		mvf.meta.Encryption == metapb.Encryption_NONE &&
		len(mvf.meta.NestedSources) == 0 &&
		len(mvf.meta.ClipBoundaries) == 0
}

// holeHooks returns the reader hooks that implement on-the-fly zero-fill for
// this handle, or nil when the file is ineligible. The hooks are built once
// per handle; the accumulator they share is seeded from the persisted hole
// map so replay pre-pad works across opens.
func (mvf *MetadataVirtualFile) holeHooks() *usenet.HoleHooks {
	mvf.holeOnce.Do(func() {
		if !mvf.holeEligible() {
			return
		}
		acc := &holes.Accumulator{}
		acc.Load(metadata.KnownHolesFromProto(mvf.meta.KnownHoles))
		mvf.holeAcc = acc
		// Snapshot while mvf.meta is still guaranteed non-nil (this runs on
		// the caller's own goroutine, before any detached download goroutine
		// can touch it). onHole/recordDegradedPad read these fields instead
		// of mvf.meta — see the field comments on MetadataVirtualFile.
		mvf.holeFileSize = mvf.meta.FileSize
		mvf.holeSourceNzbPath = mvf.meta.SourceNzbPath
		mvf.holeTotalSegments = len(mvf.meta.SegmentData)
		mvf.holeHooksVal = &usenet.HoleHooks{
			OnHole:     mvf.onHole,
			KnownHoles: mvf.isKnownHole,
		}
	})
	return mvf.holeHooksVal
}

// isKnownHole reports whether a segment is already in the hole map (replay
// pre-pad: zero-fill without a fetch round-trip).
func (mvf *MetadataVirtualFile) isKnownHole(segIndex int) bool {
	mvf.holeMu.Lock()
	defer mvf.holeMu.Unlock()
	return mvf.holeAcc.Has(segIndex)
}

// onHole is the synchronous pad/fail verdict for a segment just confirmed
// missing on every provider. It merges the miss into the handle's
// accumulator, applies the threshold table, and — when the file remains
// within the padding caps — records the file as degraded off the hot path.
// Runs on download goroutines: no network, no blocking I/O.
func (mvf *MetadataVirtualFile) onHole(segIndex int, segID string) holes.Decision {
	mvf.holeMu.Lock()
	alreadyKnown := mvf.holeAcc.Has(segIndex)
	mvf.holeAcc.Add(segIndex)
	runs := mvf.holeAcc.Runs()
	total := mvf.holeAcc.Total()
	longest := mvf.holeAcc.LongestRun()
	mvf.holeMu.Unlock()

	// Use the immutable snapshot captured in holeHooks(), not mvf.meta.
	// This function runs on a detached per-segment download goroutine that
	// Close() does not wait for; Close() sets mvf.meta = nil with no
	// synchronization against this goroutine, so reading mvf.meta here (even
	// before spawning recordDegradedPad below) would still race it.
	fileSize := mvf.holeFileSize
	sourceNzbPath := mvf.holeSourceNzbPath
	totalSegments := mvf.holeTotalSegments

	segBytes := avgSegBytes(fileSize, totalSegments)
	verdict := holes.Classify(runs, fileSize, segBytes)
	if verdict != holes.VerdictDegraded {
		// Caps exceeded: fail the stream; the DataCorruptionError path takes
		// over (repair trigger, safety-folder move) as it always has.
		slog.WarnContext(mvf.ctx, "Missing segment exceeds padding caps, failing stream",
			"file", mvf.name,
			"segment_id", segID,
			"total_missing", total,
			"longest_run", longest)
		return holes.DecisionFail
	}

	// Record the degradation without stalling the download goroutine. Replays
	// of already-known holes change nothing, so only new discoveries write.
	if !alreadyKnown {
		go mvf.recordDegradedPad(segIndex, sourceNzbPath, fileSize, total, longest, totalSegments, segBytes)
	}
	return holes.DecisionPad
}

// recordDegradedPad persists a newly padded hole and marks the file degraded:
// hole map merged into metadata, FILE_STATUS_DEGRADED (stays visible and
// streamable), health record degraded. Deliberately NO repair trigger, NO
// safety-folder move and NO masking-counter increment — the file still plays.
// Status writes are debounced per file so a burst of pads writes once per
// window; the hole itself is always persisted (idempotent merge).
//
// Runs detached (go mvf.recordDegradedPad(...) in onHole) on its own
// goroutine that outlives the per-segment download goroutine's own recover().
// A caller may Close() the handle — which sets mvf.meta = nil — at any point
// while this runs, so it must not touch mvf.meta; callers pass in everything
// derived from meta instead. The recover below is defense-in-depth so an
// unforeseen panic here logs and dies quietly rather than crashing the
// process, matching the per-segment download goroutine's own recover in
// usenet.UsenetReader.
func (mvf *MetadataVirtualFile) recordDegradedPad(segIndex int, sourceNzbPath string, fileSize int64, total, longest, totalSegments int, segBytes int64) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in recordDegradedPad", "file", mvf.name, "panic", r)
		}
	}()

	// Always persist the hole so the next open pre-pads it without a fetch.
	if err := mvf.metadataService.AddKnownHoles(mvf.name, []holes.Run{{Start: segIndex, Count: 1}}); err != nil {
		slog.WarnContext(mvf.ctx, "Failed to persist known hole", "file", mvf.name, "error", err)
	}

	// Distinct debounce key from the repair path so pads never consume a
	// repair-trigger token.
	if !mvf.repairCoalescer.ShouldTrigger(mvf.name + "\x00degraded-pad") {
		return
	}

	ctx, cancel := context.WithTimeout(mvf.ctx, 5*time.Second)
	defer cancel()

	if err := mvf.metadataService.UpdateFileStatus(mvf.name, metapb.FileStatus_FILE_STATUS_DEGRADED); err != nil {
		slog.WarnContext(ctx, "Failed to update metadata status to degraded", "file", mvf.name, "error", err)
	}

	details := database.HealthErrorDetails{
		ErrorType:       "ArticleNotFound",
		MissingArticles: total,
		TotalArticles:   totalSegments,
		PlaybackImpact: &holes.Impact{
			Verdict:       holes.VerdictDegraded,
			TotalMissing:  total,
			LongestRun:    longest,
			TotalSegments: totalSegments,
			PaddedRatio:   paddedRatio(total, segBytes, fileSize),
		},
	}
	errorMsg := "missing segments zero-filled during streaming"
	var sourceNzbPathPtr *string
	if sourceNzbPath != "" {
		sourceNzbPathPtr = &sourceNzbPath
	}

	slog.InfoContext(ctx, "Zero-filled missing segment during streaming, file marked degraded",
		"file", mvf.name,
		"total_missing", total,
		"longest_run", longest)

	if err := mvf.healthRepository.UpdateFileHealthScheduled(ctx,
		mvf.name,
		database.HealthStatusDegraded,
		&errorMsg,
		sourceNzbPathPtr,
		details.Marshal(),
		false, // no immediate scheduling — periodic re-check refines the verdict
		time.Now().UTC(),
	); err != nil {
		slog.WarnContext(ctx, "Failed to record degraded status for padded file", "file", mvf.name, "error", err)
	}
}

// classifyStreamingFailure builds the playback-impact summary for a stream
// that FAILED on a missing article (hooks absent, or pad caps exceeded).
// Returns nil for ineligible files or non-hole failures (yEnc corruption,
// pool errors), which follow the plain corruption path.
func (mvf *MetadataVirtualFile) classifyStreamingFailure(dcErr *usenet.DataCorruptionError) *holes.Impact {
	if !mvf.holeEligible() || !usenet.IsArticleNotFound(dcErr.UnderlyingErr) {
		return nil
	}

	var acc holes.Accumulator
	acc.Load(metadata.KnownHolesFromProto(mvf.meta.KnownHoles))
	mvf.holeMu.Lock()
	if mvf.holeAcc != nil {
		acc.Load(mvf.holeAcc.Runs())
	}
	mvf.holeMu.Unlock()

	// Fold in the failing segment when its position is known.
	if dcErr.FileOffset >= 0 {
		idx := buildSegmentIndex(mvf.meta.SegmentData)
		if segIdx := idx.findSegmentForOffset(dcErr.FileOffset); segIdx >= 0 {
			acc.Add(segIdx)
		}
	}

	totalSegments := len(mvf.meta.SegmentData)
	segBytes := avgSegBytes(mvf.meta.FileSize, totalSegments)
	return &holes.Impact{
		Verdict:       holes.Classify(acc.Runs(), mvf.meta.FileSize, segBytes),
		TotalMissing:  acc.Total(),
		LongestRun:    acc.LongestRun(),
		TotalSegments: totalSegments,
		PaddedRatio:   paddedRatio(acc.Total(), segBytes, mvf.meta.FileSize),
	}
}

// avgSegBytes estimates the decoded segment size for the byte-ratio guard.
func avgSegBytes(fileSize int64, totalSegments int) int64 {
	if totalSegments <= 0 || fileSize <= 0 {
		return 1
	}
	return fileSize / int64(totalSegments)
}

// paddedRatio is missing bytes over file bytes (0 when size is unknown).
func paddedRatio(total int, segBytes, fileSize int64) float64 {
	if fileSize <= 0 {
		return 0
	}
	return float64(int64(total)*segBytes) / float64(fileSize)
}
