package validation

import (
	"fmt"

	"github.com/javi11/altmount/internal/encryption/rclone"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// ValidateSegmentsForFile performs local structural validation of file segments including size
// verification. It validates that segments are structurally sound (valid offsets, non-empty IDs)
// and that their total size matches the expected file size (accounting for encryption overhead).
// Network reachability is handled solely by the fast-fail pass at import start.
func ValidateSegmentsForFile(
	filename string,
	fileSize int64,
	segments []*metapb.SegmentData,
	encryption metapb.Encryption,
) error {
	if len(segments) == 0 {
		return fmt.Errorf("no segments provided for file %s", filename)
	}

	// Single pass: structural validation + size accumulation.
	var totalSegmentSize int64
	for i, segment := range segments {
		if segment == nil {
			return fmt.Errorf("segment %d is nil for file %s", i, filename)
		}

		if segment.StartOffset < 0 || segment.EndOffset < 0 {
			return fmt.Errorf("invalid offsets (start=%d, end=%d) in segment %d for file %s",
				segment.StartOffset, segment.EndOffset, i, filename)
		}

		if segment.StartOffset > segment.EndOffset {
			return fmt.Errorf("start offset greater than end offset (start=%d, end=%d) in segment %d for file %s",
				segment.StartOffset, segment.EndOffset, i, filename)
		}

		segSize := segment.EndOffset - segment.StartOffset + 1
		if segSize <= 0 {
			return fmt.Errorf("non-positive size %d in segment %d for file %s", segSize, i, filename)
		}

		if segment.Id == "" {
			return fmt.Errorf("empty message ID in segment %d for file %s (cannot retrieve data)", i, filename)
		}

		totalSegmentSize += segSize
	}

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
