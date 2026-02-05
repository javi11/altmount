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

	// Verify that the connection pool is available
	usenetPool, err := poolManager.GetPool()
	if err != nil {
		return fmt.Errorf("cannot validate segments: usenet connection pool unavailable: %w", err)
	}

	if usenetPool == nil {
		return fmt.Errorf("cannot validate segments: usenet connection pool is nil")
	}

	// Select which segments to validate
	segmentsToValidate := selectSegmentsForValidation(segments, samplePercentage)
	totalToValidate := len(segmentsToValidate)

	// Atomic counter for progress tracking (thread-safe for concurrent validation)
	var validatedCount int32

	// Validate segments concurrently with connection limit
	pl := concpool.New().WithErrors().WithFirstError().WithMaxGoroutines(maxConnections)
	for _, segment := range segmentsToValidate {
		seg := segment // Capture loop variable
		pl.Go(func() error {
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			var err error
			if verifyData {
				// Hybrid mode: attempt to read a few bytes of the segment body
				lw := &limitedWriter{limit: 1}
				_, err = usenetPool.Body(checkCtx, seg.Id, lw, []string{})

				if errors.Is(err, ErrLimitReached) {
					err = nil
				}
			} else {
				// Standard mode: only perform STAT command
				_, err = usenetPool.Stat(checkCtx, seg.Id, []string{})
			}
			if err != nil {
				return fmt.Errorf("segment with ID %s unreachable: %w", seg.Id, err)
			}

			// Update progress after successful validation
			if progressTracker != nil {
				count := atomic.AddInt32(&validatedCount, 1)
				progressTracker.Update(int(count), totalToValidate)
			}

			return nil
		})
	}

	if err := pl.Wait(); err != nil {
		return err
	}

	return nil
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

	// Verify that the connection pool is available
	usenetPool, err := poolManager.GetPool()
	if err != nil {
		return result, fmt.Errorf("cannot validate segments: usenet connection pool unavailable: %w", err)
	}

	if usenetPool == nil {
		return result, fmt.Errorf("cannot validate segments: usenet connection pool is nil")
	}

	// Select which segments to validate
	segmentsToValidate := selectSegmentsForValidation(segments, samplePercentage)
	result.TotalChecked = len(segmentsToValidate)

	// Atomic counter for progress tracking (thread-safe for concurrent validation)
	var validatedCount int32
	var missingCount int32

	// Mutex for missing IDs collection
	// We use a channel to collect missing IDs to avoid locking
	missingChan := make(chan string, len(segmentsToValidate))

	// Validate segments concurrently with connection limit
	// We don't use WithFirstError because we want to check all selected segments
	pl := concpool.New().WithErrors().WithMaxGoroutines(maxConnections)
	for _, segment := range segmentsToValidate {
		seg := segment // Capture loop variable
		pl.Go(func() error {
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			var err error
			if verifyData {
				// Hybrid mode: attempt to read a few bytes of the segment body
				// to ensure the provider actually has the data.
				// We use a limited writer to only read 1 byte.
				lw := &limitedWriter{limit: 1}
				_, err = usenetPool.Body(checkCtx, seg.Id, lw, []string{})

				// If we reached our 1-byte limit, it means the segment data is accessible.
				if errors.Is(err, ErrLimitReached) {
					err = nil
				}
			} else {
				// Standard mode: only perform STAT command
				_, err = usenetPool.Stat(checkCtx, seg.Id, []string{})
			}
			if err != nil {
				atomic.AddInt32(&missingCount, 1)
				missingChan <- seg.Id
				// We return nil here because we are collecting errors manually
				// and we want other goroutines to continue
				return nil
			}

			// Update progress after successful validation
			if progressTracker != nil {
				count := atomic.AddInt32(&validatedCount, 1)
				progressTracker.Update(int(count), result.TotalChecked)
			}

			return nil
		})
	}

	// Wait for all checks to complete
	// We ignore the error return because we handled errors inside the goroutine
	_ = pl.Wait()
	close(missingChan)

	// Collect results
	result.MissingCount = int(missingCount)
	for id := range missingChan {
		// Limit the number of IDs we store to avoid huge metadata blobs
		if len(result.MissingIDs) < 50 {
			result.MissingIDs = append(result.MissingIDs, id)
		}
	}

	return result, nil
}

// limitedWriter is an io.Writer that stops after reaching a certain byte limit
type limitedWriter struct {
	limit int64
	read  int64
}

func (lw *limitedWriter) Write(p []byte) (n int, err error) {
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

	// Calculate target number of segments based on percentage
	targetSamples := (totalSegments * samplePercentage) / 100

	// Enforce minimum of 5 segments for statistical validity
	if targetSamples < 5 {
		targetSamples = 5
	}

	// Optimization: Cap the number of samples for very large files to prevent
	// excessive network I/O. 50 random samples + 5 fixed samples is plenty
	// for a reliable health check even on 100GB+ files.
	if targetSamples > 55 {
		targetSamples = 55
	}

	// If target samples equals or exceeds total segments, validate all
	if targetSamples >= totalSegments {
		return segments
	}

	var toValidate []*metapb.SegmentData

	// 1. First 3 segments (DMCA/takedown detection)
	firstCount := 3
	if firstCount > totalSegments {
		firstCount = totalSegments
	}
	for i := 0; i < firstCount; i++ {
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
		// Calculate how many middle segments we need to reach target
		currentCount := len(toValidate)
		randomSamples := targetSamples - currentCount

		if randomSamples > middleRange {
			randomSamples = middleRange
		}

		if randomSamples > 0 {
			// Random sampling without replacement from middle section
			perm := rand.Perm(middleRange)
			for i := 0; i < randomSamples; i++ {
				toValidate = append(toValidate, segments[middleStart+perm[i]])
			}
		}
	}

	return toValidate
}
