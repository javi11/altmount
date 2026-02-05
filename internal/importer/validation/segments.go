package validation

import (
	"context"
	"fmt"
	"time"

	"github.com/javi11/altmount/internal/encryption/rclone"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	"github.com/javi11/altmount/internal/usenet"
)

// ValidateSegmentsForFile performs comprehensive validation of file segments including size verification
// and reachability checks. It validates that segments are structurally sound, accessible via
// the Usenet connection pool, and that their total size matches the expected file size (accounting
// for encryption overhead).
//
// The optional progressTracker is passed through to segment availability validation for real-time
// progress updates during concurrent validation.
func ValidateSegmentsForFile(
	ctx context.Context,
	filename string,
	fileSize int64,
	segments []*metapb.SegmentData,
	encryption metapb.Encryption,
	poolManager pool.Manager,
	maxGoroutines int,
	samplePercentage int,
	progressTracker progress.ProgressTracker,
	timeout time.Duration,
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

	// Validate segment availability using shared validation logic
	if err := usenet.ValidateSegmentAvailability(ctx, segments, poolManager, maxGoroutines, samplePercentage, progressTracker, timeout, false); err != nil {
		return err
	}

	// For encrypted files, convert decrypted size to encrypted size for comparison
	expectedSize := fileSize
	switch encryption {
	case metapb.Encryption_RCLONE:
		expectedSize = rclone.EncryptedSize(fileSize)
	case metapb.Encryption_AES:
		// AES-CBC pads to 16-byte block boundary
		const aesBlockSize = 16
		if fileSize%aesBlockSize != 0 {
			expectedSize = fileSize + (aesBlockSize - (fileSize % aesBlockSize))
		}
	}

	if totalSegmentSize != expectedSize {
		sizeType := "decrypted"
		if encryption == metapb.Encryption_RCLONE || encryption == metapb.Encryption_AES {
			sizeType = "encrypted"
		}

		return fmt.Errorf("file '%s' is incomplete: expected %d bytes (%s) but found %d bytes (missing %d bytes)",
			filename, expectedSize, sizeType, totalSegmentSize, expectedSize-totalSegmentSize)
	}

	return nil
}
