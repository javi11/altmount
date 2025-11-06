package usenet

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
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
// Returns an error if any segment is unreachable or if the pool is unavailable.
func ValidateSegmentAvailability(
	ctx context.Context,
	segments []*metapb.SegmentData,
	poolManager pool.Manager,
	maxConnections int,
	fullValidation bool,
	samplePercentage int,
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
	segmentsToValidate := selectSegmentsForValidation(segments, fullValidation, samplePercentage)

	// Validate segments concurrently with connection limit
	pl := concpool.New().WithErrors().WithFirstError().WithMaxGoroutines(maxConnections)
	for _, segment := range segmentsToValidate {
		seg := segment // Capture loop variable
		pl.Go(func() error {
			checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			_, err := usenetPool.Stat(checkCtx, seg.Id, []string{})
			if err != nil {
				return fmt.Errorf("segment with ID %s unreachable: %w", seg.Id, err)
			}

			return nil
		})
	}

	if err := pl.Wait(); err != nil {
		return err
	}

	return nil
}

// selectSegmentsForValidation determines which segments to validate based on validation mode and sample percentage.
// For full validation, returns all segments. For sampling, uses a strategic approach that:
// - Validates first 3 segments (DMCA/takedown detection)
// - Validates last 2 segments (incomplete upload detection)
// - Validates random middle segments based on samplePercentage (general integrity check)
// A minimum of 5 segments are always validated for statistical validity when sampling.
func selectSegmentsForValidation(segments []*metapb.SegmentData, fullValidation bool, samplePercentage int) []*metapb.SegmentData {
	if fullValidation {
		return segments
	}

	totalSegments := len(segments)

	// Calculate target number of segments based on percentage
	targetSamples := (totalSegments * samplePercentage) / 100

	// Enforce minimum of 5 segments for statistical validity
	if targetSamples < 5 {
		targetSamples = 5
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
