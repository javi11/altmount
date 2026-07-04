package health

import (
	"context"
	"log/slog"

	"github.com/javi11/altmount/internal/holes"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/usenet"
)

// classifyHoles turns a check's missing segments into a playback-impact
// verdict using the hole model (see internal/holes): merge newly observed
// misses with the file's persisted hole map, then apply the threshold table.
// A full sweep can prove clean; a sampled sweep only projects (never clean).
//
// It re-reads metadata rather than taking a healthCheckInput from the caller:
// prepareCheck deliberately drops the segment slice after sampling so the
// proto is collectible during the (possibly cross-file, batched) network
// sweep, and re-reading only pays a disk read in the rare case a file
// actually has missing segments. Newly observed holes are persisted on
// degraded verdicts so playback pre-pads them without a network round-trip.
//
// Returns nil when the file is ineligible (non-video, encrypted, remuxed) or
// metadata can no longer be read.
func (hc *HealthChecker) classifyHoles(
	ctx context.Context,
	filePath string,
	result usenet.ValidationResult,
) *holes.Impact {
	if len(result.MissingIDs) == 0 {
		return nil
	}
	input, ok := hc.loadClassificationInput(filePath)
	if !ok {
		return nil
	}

	var acc holes.Accumulator
	acc.Load(metadata.KnownHolesFromProto(input.knownHoles))
	priorTotal := acc.Total()
	observed := missingRuns(input.segments, result.MissingIDs)
	acc.Load(observed)

	totalSegments := len(input.segments)
	segBytes := avgSegmentBytes(input.fileSize, totalSegments)
	fullCheck := result.TotalChecked >= totalSegments

	var verdict holes.Verdict
	if fullCheck {
		verdict = holes.Classify(acc.Runs(), input.fileSize, segBytes)
	} else {
		// Sampled evidence projects; the persisted map still counts as
		// measured damage, so take the worse of projection and accumulation.
		verdict = holes.ClassifyProjected(result.MissingCount, result.TotalChecked, totalSegments, acc.LongestRun())
		if holes.Classify(acc.Runs(), input.fileSize, segBytes) == holes.VerdictFailed {
			verdict = holes.VerdictFailed
		}
	}

	// Persist newly observed holes so playback replay pre-pads them. Only on
	// degraded verdicts: failed files head to repair/re-download anyway.
	if verdict == holes.VerdictDegraded && acc.Total() > priorTotal {
		if err := hc.metadataService.AddKnownHoles(filePath, observed); err != nil {
			slog.WarnContext(ctx, "Failed to persist known holes",
				"file_path", filePath,
				"error", err)
		}
	}

	var ratio float64
	if input.fileSize > 0 {
		ratio = float64(int64(acc.Total())*segBytes) / float64(input.fileSize)
	}
	return &holes.Impact{
		Verdict:       verdict,
		TotalMissing:  acc.Total(),
		LongestRun:    acc.LongestRun(),
		Sampled:       result.TotalChecked,
		TotalSegments: totalSegments,
		PaddedRatio:   ratio,
	}
}

// loadClassificationInput re-reads metadata for hole classification and
// reports whether the file is eligible (plain unencrypted video).
func (hc *HealthChecker) loadClassificationInput(filePath string) (healthCheckInput, bool) {
	fileMeta, err := hc.metadataService.ReadFileMetadata(filePath)
	if err != nil || fileMeta == nil {
		return healthCheckInput{}, false
	}
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
	if !holes.EligibleFile(filePath) ||
		input.encryption != metapb.Encryption_NONE ||
		input.hasNestedOrRemuxedSources {
		return healthCheckInput{}, false
	}
	return input, true
}

// missingRuns folds missing message IDs into hole runs by resolving each ID
// to its index in the file's segment list. The batch health-check path
// reports only IDs, so the mapping happens here. Sampling gaps mean observed
// runs are lower bounds on the real damage.
func missingRuns(segments []*metapb.SegmentData, missingIDs []string) []holes.Run {
	indexByID := make(map[string]int, len(segments))
	for i, seg := range segments {
		indexByID[seg.Id] = i
	}
	var acc holes.Accumulator
	for _, id := range missingIDs {
		if idx, ok := indexByID[id]; ok {
			acc.Add(idx)
		}
	}
	return acc.Runs()
}

// avgSegmentBytes estimates the decoded segment size for the byte-ratio
// guard; encoded/decoded skew is negligible at 2%.
func avgSegmentBytes(fileSize int64, totalSegments int) int64 {
	if totalSegments <= 0 || fileSize <= 0 {
		return 1
	}
	return fileSize / int64(totalSegments)
}
