package usenet

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	concpool "github.com/sourcegraph/conc/pool"
)

var randPerm = rand.Perm

// ValidateSegmentAvailability validates that segments are available on Usenet servers.
// It uses a strategic sampling approach for efficiency when fullValidation is false:
// - Validates first 3 segments (DMCA/takedown detection)
// - Validates last 2 segments (incomplete upload detection)
// - Validates random middle segments based on samplePercentage (general integrity check)
// The samplePercentage parameter controls how many segments to check (1-100%).
//
// For fullValidation=true, all segments are validated regardless of samplePercentage.
// A minimum of 5 segments are always validated for statistical validity when sampling.
//
// The optional progressTracker updates progress after each segment validation completes,
// providing real-time progress updates during concurrent validation.
//
// Returns an error if any segment is unreachable or if the pool is unavailable.
func ValidateSegmentAvailability(
	ctx context.Context,
	segments []*metapb.SegmentData,
	poolManager pool.Manager,
	maxConnections int,
	samplePercentage int,
	progressTracker progress.ProgressTracker,
	timeout time.Duration,
	verifyData bool,
) error {
	if len(segments) == 0 {
		return nil
	}

	selected := selectSegmentsForValidation(segments, samplePercentage)
	return ValidateSegmentList(ctx, selected, poolManager, maxConnections, progressTracker, timeout, verifyData)
}

// ValidateSegmentList validates a pre-selected list of segments via Usenet.
// Unlike ValidateSegmentAvailability, no additional sampling/selection is performed —
// every segment in the list is checked. Callers that pre-compute their own selection
// (e.g. after combining structural validation + selection in a single pass) should use
// this function to avoid a redundant iteration over the full segment slice.
func ValidateSegmentList(
	ctx context.Context,
	segments []*metapb.SegmentData,
	poolManager pool.Manager,
	maxConnections int,
	progressTracker progress.ProgressTracker,
	timeout time.Duration,
	verifyData bool,
) error {
	if len(segments) == 0 {
		return nil
	}

	usenetPool, err := poolManager.GetPool()
	if err != nil {
		return fmt.Errorf("cannot validate segments: usenet connection pool unavailable: %w", err)
	}

	if usenetPool == nil {
		return fmt.Errorf("cannot validate segments: usenet connection pool is nil")
	}

	totalToValidate := len(segments)
	var validatedCount int32

	pl := concpool.New().WithErrors().WithFirstError().WithMaxGoroutines(maxConnections)
	for _, seg := range segments {
		pl.Go(func() error {
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			var err error
			if verifyData {
				lw := &limitedWriter{limit: 1}
				_, err = usenetPool.BodyStream(checkCtx, seg.Id, lw)

				if errors.Is(err, ErrLimitReached) {
					err = nil
				}

				if err == nil {
					poolManager.IncArticlesDownloaded()
					poolManager.UpdateDownloadProgress("", lw.read)
					if !lw.hasData {
						err = fmt.Errorf("segment with ID %s contains only zero bytes", seg.Id)
					}
				}
			} else {
				_, err = usenetPool.Stat(checkCtx, seg.Id)
				if err == nil {
					poolManager.IncArticlesDownloaded()
					poolManager.UpdateDownloadProgress("", 100)
				}
			}
			if err != nil {
				return fmt.Errorf("segment with ID %s unreachable: %w", seg.Id, err)
			}

			if progressTracker != nil {
				count := atomic.AddInt32(&validatedCount, 1)
				progressTracker.Update(int(count), totalToValidate)
			}

			return nil
		})
	}

	return pl.Wait()
}

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
	verifyData bool,
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
			if verifyData {
				lw := &limitedWriter{limit: 1}
				_, err = usenetPool.BodyStream(checkCtx, seg.Id, lw)

				if errors.Is(err, ErrLimitReached) {
					err = nil
				}

				if err == nil {
					poolManager.IncArticlesDownloaded()
					poolManager.UpdateDownloadProgress("", lw.read)
					if !lw.hasData {
						err = fmt.Errorf("segment with ID %s contains only zero bytes", seg.Id)
					}
				}
			} else {
				_, err = usenetPool.Stat(checkCtx, seg.Id)
				if err == nil {
					poolManager.IncArticlesDownloaded()
					poolManager.UpdateDownloadProgress("", 100)
				}
			}
			if err != nil {
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

// limitedWriter is an io.Writer that stops after reaching a certain byte limit
// and tracks if any non-zero bytes were seen.
type limitedWriter struct {
	limit   int64
	read    int64
	hasData bool
}

func (lw *limitedWriter) Write(p []byte) (n int, err error) {
	if !lw.hasData {
		for _, b := range p {
			if b != 0 {
				lw.hasData = true
				break
			}
		}
	}

	canWrite := lw.limit - lw.read
	if canWrite <= 0 {
		return 0, ErrLimitReached
	}

	if int64(len(p)) > canWrite {
		lw.read += canWrite
		return int(canWrite), ErrLimitReached
	}

	lw.read += int64(len(p))
	return len(p), nil
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
