package steps

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/javi11/altmount/internal/encryption/rclone"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	concpool "github.com/sourcegraph/conc/pool"
)

// ValidateParsedDataStep validates the parsed NZB/STRM structure
type ValidateParsedDataStep struct {
	parser interface{} // parser.Parser or parser.StrmParser
}

// NewValidateParsedDataStep creates a new validation step for parsed data
func NewValidateParsedDataStep(parser interface{}) *ValidateParsedDataStep {
	return &ValidateParsedDataStep{parser: parser}
}

// Execute validates the parsed data structure
func (s *ValidateParsedDataStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	// Validation is typically done by the parser itself
	// This step is a placeholder for any additional validation
	return nil
}

// Name returns the step name
func (s *ValidateParsedDataStep) Name() string {
	return "ValidateParsedData"
}

// ValidateSegmentsStep performs comprehensive validation of file segments
type ValidateSegmentsStep struct {
	poolManager             pool.Manager
	maxValidationGoroutines int
	fullSegmentValidation   bool
}

// NewValidateSegmentsStep creates a new segment validation step
func NewValidateSegmentsStep(poolManager pool.Manager, maxGoroutines int, fullValidation bool) *ValidateSegmentsStep {
	return &ValidateSegmentsStep{
		poolManager:             poolManager,
		maxValidationGoroutines: maxGoroutines,
		fullSegmentValidation:   fullValidation,
	}
}

// Execute validates segments for all files in the context
func (s *ValidateSegmentsStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	// This step validates segments - actual validation logic will be called per-file
	// during metadata creation steps
	return nil
}

// Name returns the step name
func (s *ValidateSegmentsStep) Name() string {
	return "ValidateSegments"
}

// ValidateSegmentsForFile performs comprehensive validation of file segments including size verification
// and reachability checks. It validates that segments are structurally sound, accessible via
// the Usenet connection pool, and that their total size matches the expected file size (accounting
// for encryption overhead).
func ValidateSegmentsForFile(
	filename string,
	fileSize int64,
	segments []*metapb.SegmentData,
	encryption metapb.Encryption,
	poolManager pool.Manager,
	maxGoroutines int,
	fullValidation bool,
) error {
	if len(segments) == 0 {
		return fmt.Errorf("no segments provided for file %s", filename)
	}

	// First, verify that the connection pool is available
	usenetPool, err := poolManager.GetPool()
	if err != nil {
		return fmt.Errorf("cannot write metadata for %s: usenet connection pool unavailable: %w", filename, err)
	}

	if usenetPool == nil {
		return fmt.Errorf("cannot write metadata for %s: usenet connection pool is nil", filename)
	}

	// First loop: Calculate total size from ALL segments
	totalSegmentSize := int64(0)
	for i, segment := range segments {
		if segment == nil {
			return fmt.Errorf("segment %d is nil for file %s", i, filename)
		}

		// Validate segment has valid offsets
		if segment.StartOffset < 0 || segment.EndOffset < 0 {
			return fmt.Errorf("invalid offsets (start=%d, end=%d) in segment %d for file %s",
				segment.StartOffset, segment.EndOffset, i, filename)
		}

		if segment.StartOffset > segment.EndOffset {
			return fmt.Errorf("start offset greater than end offset (start=%d, end=%d) in segment %d for file %s",
				segment.StartOffset, segment.EndOffset, i, filename)
		}

		// Calculate segment size
		segSize := segment.EndOffset - segment.StartOffset + 1
		if segSize <= 0 {
			return fmt.Errorf("non-positive size %d in segment %d for file %s", segSize, i, filename)
		}

		// Validate segment has a valid Usenet message ID
		if segment.Id == "" {
			return fmt.Errorf("empty message ID in segment %d for file %s (cannot retrieve data)", i, filename)
		}

		totalSegmentSize += segSize
	}

	// Determine which segments to validate for reachability
	var segmentsToValidate []*metapb.SegmentData
	if fullValidation {
		segmentsToValidate = segments
	} else {
		// Validate a random sample of up to 10 segments
		sampleSize := 10
		if len(segments) < sampleSize {
			sampleSize = len(segments)
		}

		segmentsToValidate = make([]*metapb.SegmentData, sampleSize)
		if sampleSize == len(segments) {
			segmentsToValidate = segments
		} else {
			// Random sampling without replacement
			perm := rand.Perm(len(segments))
			for i := 0; i < sampleSize; i++ {
				segmentsToValidate[i] = segments[perm[i]]
			}
		}
	}

	// Second loop: Validate reachability of sampled segments
	pl := concpool.New().WithErrors().WithFirstError().WithMaxGoroutines(maxGoroutines)
	for _, segment := range segmentsToValidate {
		pl.Go(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			_, err := usenetPool.Stat(ctx, segment.Id, []string{})
			if err != nil {
				return fmt.Errorf("segment with ID %s unreachable for file %s: %w", segment.Id, filename, err)
			}

			return nil
		})
	}

	if err := pl.Wait(); err != nil {
		return err
	}

	// For encrypted files, convert decrypted size to encrypted size for comparison
	expectedSize := fileSize
	if encryption == metapb.Encryption_RCLONE {
		expectedSize = rclone.EncryptedSize(fileSize)
	}

	if totalSegmentSize != expectedSize {
		sizeType := "decrypted"
		if encryption == metapb.Encryption_RCLONE {
			sizeType = "encrypted"
		}

		return fmt.Errorf("file '%s' is incomplete: expected %d bytes (%s) but found %d bytes (missing %d bytes)",
			filename, expectedSize, sizeType, totalSegmentSize, expectedSize-totalSegmentSize)
	}

	return nil
}
