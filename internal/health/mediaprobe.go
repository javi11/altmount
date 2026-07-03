package health

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/javi11/altmount/internal/mediaprobe"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/usenet"
)

// probeTimeout bounds one container probe (a handful of small header reads,
// each potentially downloading one article).
const probeTimeout = 30 * time.Second

// probeEligible reports whether a file's container can be probed/classified:
// plain (unencrypted, non-remuxed, non-nested) video files only.
func probeEligible(input healthCheckInput, filePath string) bool {
	return mediaprobe.ContainerForFile(filePath) != "" &&
		input.encryption == metapb.Encryption_NONE &&
		!input.hasNestedOrRemuxedSources
}

// missingAwareReaderAt fails reads that touch known-missing byte ranges with
// mediaprobe.ErrMissingRange instead of letting them hit the network, so a
// probe over a holed file gives up instantly rather than hanging on dead
// articles.
type missingAwareReaderAt struct {
	inner   io.ReaderAt
	missing []mediaprobe.ByteRange
}

func (r *missingAwareReaderAt) ReadAt(p []byte, off int64) (int, error) {
	req := mediaprobe.ByteRange{Start: off, End: off + int64(len(p)) - 1}
	for _, m := range r.missing {
		if req.Start <= m.End && m.Start <= req.End {
			return 0, mediaprobe.ErrMissingRange
		}
	}
	return r.inner.ReadAt(p, off)
}

// classifyPlaybackImpact determines whether the missing segments found by a
// health check break playback. It re-reads metadata rather than taking a
// healthCheckInput from the caller: prepareCheck deliberately drops the
// segment slice after sampling so the proto is collectible during the
// (possibly cross-file, batched) network sweep, and re-reading only pays a
// disk read in the rare case a file actually has missing segments.
//
// Fast path: intersect against the structure persisted at import time. Lazy
// fallback (pre-feature files): probe the container live over the available
// segments, persisting the structure for next time. Returns nil when probing
// is disabled, the file is ineligible, or metadata can no longer be read.
func (hc *HealthChecker) classifyPlaybackImpact(
	ctx context.Context,
	filePath string,
	result usenet.ValidationResult,
) *mediaprobe.Classification {
	if !hc.configGetter().GetMediaProbeEnabled() || len(result.MissingIDs) == 0 {
		return nil
	}
	input, ok := hc.loadClassificationInput(filePath)
	if !ok {
		return nil
	}

	missing := resolveMissingRanges(input.segments, result.MissingIDs)
	// Captured ranges are capped; if more segments are missing than we can
	// place, a degraded verdict would be based on partial knowledge.
	if result.MissingCount > len(missing) {
		return &mediaprobe.Classification{
			Verdict:   mediaprobe.VerdictFatal,
			Container: mediaprobe.ContainerForFile(filePath),
			Reason:    fmt.Sprintf("too much media data missing (%d segments)", result.MissingCount),
		}
	}

	// Fast path: structure persisted at import time — pure intersection.
	if s := metadata.MediaStructureFromProto(input.mediaStructure, input.fileSize); s != nil {
		cls := mediaprobe.ClassifyAgainst(s, missing)
		return &cls
	}

	// Lazy fallback: live probe over the segments that are still available.
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	reader := &missingAwareReaderAt{
		inner:   hc.newSegmentsReader(probeCtx, input),
		missing: missing,
	}
	s, err := mediaprobe.Probe(probeCtx, reader, input.fileSize, filePath)
	if err != nil {
		slog.DebugContext(ctx, "Container probe failed during classification",
			"file_path", filePath,
			"error", err)
		return &mediaprobe.Classification{
			Verdict:       mediaprobe.VerdictUnknown,
			Container:     mediaprobe.ContainerForFile(filePath),
			Reason:        fmt.Sprintf("container probe failed: %v", err),
			MissingRanges: missing,
		}
	}
	hc.persistStructure(ctx, filePath, s)
	cls := mediaprobe.ClassifyAgainst(s, missing)
	return &cls
}

// maybeProbeStructure runs the one-time post-import container probe: when a
// healthy video file has no persisted MediaStructure yet, map its layout now
// (segments are freshest right after import) so a future failure classifies
// offline. Failures are logged and ignored — the health verdict is unaffected.
func (hc *HealthChecker) maybeProbeStructure(ctx context.Context, filePath string) {
	if !hc.configGetter().GetMediaProbeEnabled() {
		return
	}
	input, ok := hc.loadClassificationInput(filePath)
	if !ok || input.mediaStructure != nil {
		return // ineligible, unreadable, or already probed
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	s, err := mediaprobe.Probe(probeCtx, hc.newSegmentsReader(probeCtx, input), input.fileSize, filePath)
	if err != nil {
		slog.DebugContext(ctx, "Post-import container probe failed",
			"file_path", filePath,
			"error", err)
		return
	}
	hc.persistStructure(ctx, filePath, s)
}

// loadClassificationInput re-reads metadata for classification/probing
// purposes and reports whether the file is eligible for either.
func (hc *HealthChecker) loadClassificationInput(filePath string) (healthCheckInput, bool) {
	fileMeta, err := hc.metadataService.ReadFileMetadata(filePath)
	if err != nil || fileMeta == nil {
		return healthCheckInput{}, false
	}
	input := healthCheckInput{
		fileSize:       fileMeta.FileSize,
		sourceNzbPath:  fileMeta.SourceNzbPath,
		segments:       fileMeta.SegmentData,
		encryption:     fileMeta.Encryption,
		mediaStructure: fileMeta.MediaStructure,
		hasNestedOrRemuxedSources: len(fileMeta.NestedSources) > 0 ||
			len(fileMeta.SharedOuterSources) > 0 ||
			len(fileMeta.ClipBoundaries) > 0,
	}
	if !probeEligible(input, filePath) {
		return healthCheckInput{}, false
	}
	return input, true
}

// resolveMissingRanges maps missing message IDs back to their file-coordinate
// byte ranges via a prefix sum over the full (unsampled) segment list.
func resolveMissingRanges(segments []*metapb.SegmentData, missingIDs []string) []mediaprobe.ByteRange {
	indexByID := make(map[string]int, len(segments))
	offsets := make([]int64, len(segments))
	var pos int64
	for i, seg := range segments {
		indexByID[seg.Id] = i
		offsets[i] = pos
		pos += seg.EndOffset - seg.StartOffset + 1
	}

	ranges := make([]mediaprobe.ByteRange, 0, len(missingIDs))
	for _, id := range missingIDs {
		idx, ok := indexByID[id]
		if !ok {
			continue
		}
		seg := segments[idx]
		ranges = append(ranges, mediaprobe.ByteRange{
			Start: offsets[idx],
			End:   offsets[idx] + (seg.EndOffset - seg.StartOffset),
		})
	}
	return ranges
}

func (hc *HealthChecker) persistStructure(ctx context.Context, filePath string, s *mediaprobe.Structure) {
	if err := hc.metadataService.UpdateMediaStructure(filePath, metadata.MediaStructureToProto(s)); err != nil {
		slog.WarnContext(ctx, "Failed to persist media structure",
			"file_path", filePath,
			"error", err)
		return
	}
	slog.InfoContext(ctx, "Persisted media structure for playback-impact classification",
		"file_path", filePath,
		"container", s.Container,
		"duration_seconds", s.DurationSeconds)
}

func (hc *HealthChecker) newSegmentsReader(ctx context.Context, input healthCheckInput) io.ReaderAt {
	loader := &metadataSegmentLoader{segments: input.segments}
	return usenet.NewSegmentsReaderAt(ctx, loader, hc.poolManager.GetPool, hc.poolManager, input.fileSize)
}
