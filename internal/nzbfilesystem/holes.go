package nzbfilesystem

import (
	"log/slog"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/holes"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/usenet"
)

// holeMetaSnapshot holds the mvf.meta fields the hole path needs, captured
// once in holeHooks() on the caller's own goroutine while mvf.meta is still
// guaranteed non-nil. onHole runs on detached per-segment download goroutines
// that Close() does not wait for, and Close() sets mvf.meta = nil with no
// synchronization against them — so the hole path must never read mvf.meta
// directly; this snapshot is its only race-free access.
type holeMetaSnapshot struct {
	fileSize      int64
	sourceNzbPath string
	totalSegments int
	// fingerprint names the provider set this handle's holes are judged
	// against; persisted with every run it records.
	fingerprint string
}

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

// PatchSource serves repaired article payloads by message ID (implemented by
// par2repair.PatchStore). Must be fast local I/O and concurrency-safe.
type PatchSource interface {
	Get(messageID string) ([]byte, bool)
}

// holeHooks returns the reader hooks that implement on-the-fly zero-fill for
// this handle, or nil when the file is ineligible. The hooks are built once
// per handle; the accumulator they share is seeded from the persisted hole
// map so replay pre-pad works across opens.
//
// Files ineligible for zero-fill (encrypted, nested, non-video) still get
// hooks carrying only PatchLookup when a patch source is configured: their
// streams fail on new damage exactly as before, but articles repaired by a
// PAR2 job serve byte-exact.
func (mvf *MetadataVirtualFile) holeHooks() *usenet.HoleHooks {
	mvf.holeOnce.Do(func() {
		if !mvf.holeEligible() {
			if mvf.patchSource != nil {
				mvf.holeHooksVal = &usenet.HoleHooks{PatchLookup: mvf.patchLookup}
			}
			return
		}
		fingerprint := ""
		if mvf.configGetter != nil {
			fingerprint = mvf.configGetter().ProviderFingerprint()
		}
		acc := &holes.Accumulator{}
		// Only holes confirmed recently, and against the providers configured
		// now, are replayed without a fetch; anything else is asked for again.
		acc.Load(metadata.LiveKnownHoles(mvf.meta.KnownHoles, mvf.meta.HoleProviderFingerprint, fingerprint, time.Now()))
		mvf.holeAcc = acc
		// Snapshot before any detached download goroutine exists — see the
		// holeMetaSnapshot doc comment for why onHole must not read mvf.meta.
		mvf.holeMeta = holeMetaSnapshot{
			fileSize:      mvf.meta.FileSize,
			sourceNzbPath: mvf.meta.SourceNzbPath,
			totalSegments: len(mvf.meta.SegmentData),
			fingerprint:   fingerprint,
		}
		mvf.holeHooksVal = &usenet.HoleHooks{
			OnHole:     mvf.onHole,
			KnownHoles: mvf.isKnownHole,
		}
		if mvf.patchSource != nil {
			mvf.holeHooksVal.PatchLookup = mvf.patchLookup
		}
	})
	return mvf.holeHooksVal
}

// patchLookup adapts the patch source to the reader hook. Segment IDs are
// normalized to the bare form the patch store keys on.
func (mvf *MetadataVirtualFile) patchLookup(segID string) []byte {
	p, ok := mvf.patchSource.Get(strings.Trim(segID, "<>"))
	if !ok {
		return nil
	}
	return p
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

	// Use the immutable snapshot captured in holeHooks(), never mvf.meta —
	// see the holeMetaSnapshot doc comment.
	segBytes := avgSegBytes(mvf.holeMeta.fileSize, mvf.holeMeta.totalSegments)
	verdict := holes.Classify(runs, mvf.holeMeta.fileSize, segBytes)
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

	// Record the degradation without stalling the download goroutine: hand a
	// value-typed event to the process-lived padRecorder worker. Replays of
	// already-known holes change nothing, so only new discoveries write.
	if !alreadyKnown {
		mvf.padRecorder.enqueue(padEvent{
			name:          mvf.name,
			segIndex:      segIndex,
			segID:         segID,
			sourceNzbPath: mvf.holeMeta.sourceNzbPath,
			fileSize:      mvf.holeMeta.fileSize,
			total:         total,
			longest:       longest,
			totalSegments: mvf.holeMeta.totalSegments,
			segBytes:      segBytes,
			fingerprint:   mvf.holeMeta.fingerprint,
		})
	}
	return holes.DecisionPad
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
