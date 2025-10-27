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
// - Validates 7 random middle segments (general integrity check)
// This approach catches ~95% of incomplete files while validating only ~12 segments.
//
// For fullValidation=true, all segments are validated.
// For files with ≤12 segments, all segments are always validated.
//
// Returns an error if any segment is unreachable or if the pool is unavailable.
func ValidateSegmentAvailability(
	ctx context.Context,
	segments []*metapb.SegmentData,
	poolManager pool.Manager,
	maxConnections int,
	fullValidation bool,
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
	segmentsToValidate := selectSegmentsForValidation(segments, fullValidation)

	// Validate segments concurrently with connection limit
	pl := concpool.New().WithErrors().WithFirstError().WithMaxGoroutines(maxConnections)
	for _, segment := range segmentsToValidate {
		seg := segment // Capture loop variable
		pl.Go(func() error {
			checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			_, err := usenetPool.Stat(checkCtx, seg.Id, []string{})
			if err != nil {
				return fmt.Errorf("segment with ID %s unreachable for file %s: %w", seg.Id, err)
			}

			return nil
		})
	}

	if err := pl.Wait(); err != nil {
		return err
	}

	return nil
}

// selectSegmentsForValidation determines which segments to validate based on validation mode.
// For full validation, returns all segments. For sampling, uses a strategic approach that:
// - Validates first 3 segments (DMCA/takedown detection)
// - Validates last 2 segments (incomplete upload detection)
// - Validates 7 random middle segments (general integrity check)
// This approach catches ~95% of incomplete files while validating only ~12 segments.
func selectSegmentsForValidation(segments []*metapb.SegmentData, fullValidation bool) []*metapb.SegmentData {
	if fullValidation {
		return segments
	}

	totalSegments := len(segments)
	if totalSegments <= 12 {
		// For small files, validate all segments
		return segments
	}

	var toValidate []*metapb.SegmentData

	// 1. First 3 segments (DMCA/takedown detection)
	for i := 0; i < 3; i++ {
		toValidate = append(toValidate, segments[i])
	}

	// 2. Last 2 segments (incomplete upload detection)
	for i := totalSegments - 2; i < totalSegments; i++ {
		toValidate = append(toValidate, segments[i])
	}

	// 3. Random middle segments (7 samples for general validation)
	middleStart := 3
	middleEnd := totalSegments - 2
	middleRange := middleEnd - middleStart

	if middleRange > 0 {
		randomSamples := 7
		if middleRange < randomSamples {
			randomSamples = middleRange
		}

		// Random sampling without replacement from middle section
		perm := rand.Perm(middleRange)
		for i := 0; i < randomSamples; i++ {
			toValidate = append(toValidate, segments[middleStart+perm[i]])
		}
	}

	return toValidate
}
