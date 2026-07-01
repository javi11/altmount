package usenet

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync/atomic"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	concpool "github.com/sourcegraph/conc/pool"
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

	var validatedCount int32
	var missingCount int32
	missingChan := make(chan string, len(segmentsToValidate))

	// No WithFirstError: we want to check all selected segments, not fail fast.
	pl := concpool.New().WithErrors().WithMaxGoroutines(maxConnections)
	for _, seg := range segmentsToValidate {
		pl.Go(func() error {
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			var err error
			_, err = usenetPool.Stat(checkCtx, seg.Id)
			if err == nil {
				poolManager.IncArticlesDownloaded()
				poolManager.UpdateDownloadProgress("", 100)
			}
			if err != nil {
				slog.DebugContext(checkCtx, "missing segment",
					"segment_id", seg.Id,
					"error", err,
				)
				atomic.AddInt32(&missingCount, 1)
				missingChan <- seg.Id
				return nil // continue checking remaining segments
			}

			if progressTracker != nil {
				count := atomic.AddInt32(&validatedCount, 1)
				progressTracker.Update(int(count), result.TotalChecked)
			}

			return nil
		})
	}

	_ = pl.Wait()
	close(missingChan)

	result.MissingCount = int(missingCount)
	for id := range missingChan {
		if len(result.MissingIDs) < 50 { // cap stored IDs to avoid huge metadata blobs
			result.MissingIDs = append(result.MissingIDs, id)
		}
	}

	return result, nil
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
