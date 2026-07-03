package usenet

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	"github.com/javi11/nntppool/v4"
)

var randPerm = rand.Perm

// SelectSegmentsForValidation is the exported form of the sampling selector.
// It returns the subset of segments that should be validated based on samplePercentage,
// applying the same first-3 / last-2 / random-middle strategy used internally.
func SelectSegmentsForValidation(segments []*metapb.SegmentData, samplePercentage int) []*metapb.SegmentData {
	return selectSegmentsForValidation(segments, samplePercentage)
}

// ValidationResult holds detailed validation results
type ValidationResult struct {
	TotalChecked int
	MissingCount int
	MissingIDs   []string
}

// ValidateSegmentAvailabilityDetailed validates segments and returns detailed results
// instead of failing fast on the first error.
func ValidateSegmentAvailabilityDetailed(
	ctx context.Context,
	segments []*metapb.SegmentData,
	poolManager pool.Manager,
	maxConnections int,
	samplePercentage int,
	progressTracker progress.ProgressTracker,
	timeout time.Duration,
) (ValidationResult, error) {
	result := ValidationResult{
		MissingIDs: []string{},
	}

	if len(segments) == 0 {
		return result, nil
	}

	usenetPool, err := poolManager.GetPool()
	if err != nil {
		return result, fmt.Errorf("cannot validate segments: usenet connection pool unavailable: %w", err)
	}

	if usenetPool == nil {
		return result, fmt.Errorf("cannot validate segments: usenet connection pool is nil")
	}

	segmentsToValidate := selectSegmentsForValidation(segments, samplePercentage)
	result.TotalChecked = len(segmentsToValidate)

	if maxConnections <= 0 {
		maxConnections = 1
	}

	ids := make([]string, len(segmentsToValidate))
	for i, seg := range segmentsToValidate {
		ids[i] = seg.Id
	}

	statCtx, cancel := context.WithTimeout(ctx, pool.StatManyTimeout(len(ids), maxConnections, timeout))
	defer cancel()

	var validatedCount int
	for r := range usenetPool.StatMany(statCtx, ids, nntppool.StatManyOptions{Concurrency: maxConnections}) {
		if r.Err != nil {
			slog.DebugContext(ctx, "missing segment",
				"segment_id", r.MessageID,
				"error", r.Err,
			)
			result.MissingCount++
			if len(result.MissingIDs) < 50 { // cap stored IDs to avoid huge metadata blobs
				result.MissingIDs = append(result.MissingIDs, r.MessageID)
			}
			continue
		}

		poolManager.IncArticlesDownloaded()
		poolManager.UpdateDownloadProgress("", 100)

		if progressTracker != nil {
			validatedCount++
			progressTracker.Update(validatedCount, result.TotalChecked)
		}
	}

	return result, nil
}

// ValidateSegmentAvailabilityBatch checks pre-sampled segment IDs for many files
// in a single StatMany sweep. perFileIDs is index-aligned with the returned
// results: files with an empty ID list yield a zero ValidationResult. IDs are
// interleaved round-robin across files (every file's first sample, then every
// file's second, …) so one file with many segments cannot serialize the sweep
// for the others. An error is returned only for infrastructure failures (pool
// unavailable); per-segment misses are reported in the per-file results.
func ValidateSegmentAvailabilityBatch(
	ctx context.Context,
	perFileIDs [][]string,
	poolManager pool.Manager,
	maxConnections int,
	timeout time.Duration,
) ([]ValidationResult, error) {
	results := make([]ValidationResult, len(perFileIDs))
	for i := range results {
		results[i].MissingIDs = []string{}
		results[i].TotalChecked = len(perFileIDs[i])
	}

	total := 0
	maxSamples := 0
	for _, ids := range perFileIDs {
		total += len(ids)
		if len(ids) > maxSamples {
			maxSamples = len(ids)
		}
	}
	if total == 0 {
		return results, nil
	}

	usenetPool, err := poolManager.GetPool()
	if err != nil {
		return results, fmt.Errorf("cannot validate segments: usenet connection pool unavailable: %w", err)
	}
	if usenetPool == nil {
		return results, fmt.Errorf("cannot validate segments: usenet connection pool is nil")
	}

	if maxConnections <= 0 {
		maxConnections = 1
	}

	// Round-robin interleave IDs across files. Results stream out of order from
	// StatMany, so ownership is resolved by ID: owners maps each message-id to
	// the file indexes that reference it (FIFO pop per result, so duplicate IDs
	// shared across files each get their own attribution).
	ids := make([]string, 0, total)
	owners := make(map[string][]int, total)
	for round := 0; round < maxSamples; round++ {
		for fileIdx, fileIDs := range perFileIDs {
			if round < len(fileIDs) {
				id := fileIDs[round]
				ids = append(ids, id)
				owners[id] = append(owners[id], fileIdx)
			}
		}
	}

	statCtx, cancel := context.WithTimeout(ctx, pool.StatManyTimeout(len(ids), maxConnections, timeout))
	defer cancel()

	nonEmptyFiles := 0
	for _, fileIDs := range perFileIDs {
		if len(fileIDs) > 0 {
			nonEmptyFiles++
		}
	}

	sweepStart := time.Now()
	slog.InfoContext(ctx, "Starting STAT sweep",
		"files", nonEmptyFiles,
		"segments", len(ids),
		"concurrency", maxConnections,
	)

	for r := range usenetPool.StatMany(statCtx, ids, nntppool.StatManyOptions{Concurrency: maxConnections}) {
		owner := owners[r.MessageID]
		if len(owner) == 0 {
			// Unknown ID — should not happen, but never panic on pool output.
			continue
		}
		fileIdx := owner[0]
		owners[r.MessageID] = owner[1:]

		if r.Err != nil {
			slog.DebugContext(ctx, "missing segment",
				"segment_id", r.MessageID,
				"error", r.Err,
			)
			results[fileIdx].MissingCount++
			if len(results[fileIdx].MissingIDs) < 50 { // cap stored IDs to avoid huge metadata blobs
				results[fileIdx].MissingIDs = append(results[fileIdx].MissingIDs, r.MessageID)
			}
			continue
		}

		poolManager.IncArticlesDownloaded()
		poolManager.UpdateDownloadProgress("", 100)
	}

	missingTotal := 0
	for _, r := range results {
		missingTotal += r.MissingCount
	}
	slog.InfoContext(ctx, "STAT sweep completed",
		"files", nonEmptyFiles,
		"segments", len(ids),
		"missing", missingTotal,
		"duration", time.Since(sweepStart),
	)

	return results, nil
}

// selectSegmentsForValidation determines which segments to validate based on validation mode and sample percentage.
// For full validation, returns all segments. For sampling, uses a strategic approach that:
// - Validates first 3 segments (DMCA/takedown detection)
// - Validates last 2 segments (incomplete upload detection)
// - Validates random middle segments based on samplePercentage (general integrity check)
// A minimum of 5 segments are always validated for statistical validity when sampling.
func selectSegmentsForValidation(segments []*metapb.SegmentData, samplePercentage int) []*metapb.SegmentData {
	if samplePercentage == 100 {
		return segments
	}

	totalSegments := len(segments)

	// Min 5 for statistical validity, max 55 to cap network I/O on large files.
	targetSamples := min(max((totalSegments*samplePercentage)/100, 5), 55)

	if targetSamples >= totalSegments {
		return segments
	}

	var toValidate []*metapb.SegmentData

	// 1. First 3 segments (DMCA/takedown detection)
	firstCount := min(3, totalSegments)
	for i := range firstCount {
		toValidate = append(toValidate, segments[i])
	}

	// 2. Last 2 segments (incomplete upload detection)
	lastCount := 2
	if firstCount+lastCount > totalSegments {
		lastCount = totalSegments - firstCount
	}
	if lastCount > 0 {
		for i := totalSegments - lastCount; i < totalSegments; i++ {
			toValidate = append(toValidate, segments[i])
		}
	}

	// 3. Random middle segments to reach target sample size
	middleStart := firstCount
	middleEnd := totalSegments - lastCount
	middleRange := middleEnd - middleStart

	if middleRange > 0 {
		randomSamples := min(targetSamples-len(toValidate), middleRange)

		if randomSamples > 0 {
			perm := randPerm(middleRange)
			for i := range randomSamples {
				toValidate = append(toValidate, segments[middleStart+perm[i]])
			}
		}
	}

	return toValidate
}
