package nzbfilesystem

import (
	"context"
	"io"

	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/altmount/internal/utils"
)

// calculateSmartRange determines the optimal range for a reader based on position and constraints
func (vf *VirtualFile) calculateSmartRange(position int64) (start, end int64) {
	// Get HTTP range constraints if available
	rangeStart, rangeEnd, hasRange := vf.getRequestRange()

	if hasRange {
		// Check if HTTP range is reasonable in size
		rangeSize := rangeEnd - rangeStart + 1

		if rangeSize <= MaxChunkSize {
			// Range is reasonable, use it
			start = rangeStart
			end = rangeEnd
			if position < start {
				position = start
			}
			if position > end {
				position = end
			}
		} else {
			// Range is too large (likely end=-1 case), use chunking from current position
			// but respect the range boundaries
			start = position
			if start < rangeStart {
				start = rangeStart
			}

			// Use smart chunking within the HTTP range
			chunkSize := vf.getOptimalChunkSize()
			end = start + chunkSize - 1

			// Don't exceed the HTTP range end
			if end > rangeEnd {
				end = rangeEnd
			}
		}
	} else {
		// No HTTP range - use smart chunking to avoid memory leaks
		start = position
		chunkSize := vf.getOptimalChunkSize()
		end = start + chunkSize - 1

		if end >= vf.virtualFile.Size {
			end = vf.virtualFile.Size - 1
		}
	}

	// Final validation
	if start < 0 {
		start = 0
	}
	if end >= vf.virtualFile.Size {
		end = vf.virtualFile.Size - 1
	}
	if start > end {
		// Fallback to just the current position
		start = position
		end = position
	}

	return start, end
}

// getOptimalChunkSize returns the optimal chunk size based on file size
func (vf *VirtualFile) getOptimalChunkSize() int64 {
	fileSize := vf.virtualFile.Size

	switch {
	case fileSize < SmallFileThreshold:
		// Small files: read entire file
		return fileSize
	case fileSize < MediumFileThreshold:
		// Medium files: 10MB chunks
		return MediumFileChunkSize
	case fileSize < LargeFileThreshold:
		// Large files: 25MB chunks
		return LargeFileChunkSize
	default:
		// Very large files: 50MB chunks
		return VeryLargeFileChunkSize
	}
}

// getRequestRange extracts and validates the HTTP Range header from the original request
// Returns the effective range to use for reader creation, considering file size limits
func (vf *VirtualFile) getRequestRange() (start, end int64, hasRange bool) {
	// Try to get range from HTTP request args
	rangeHeader, err := vf.args.Range()
	if err != nil || rangeHeader == nil {
		// No valid range header, return full file range
		return 0, vf.virtualFile.Size - 1, false
	}

	// Fix range header to ensure it's within file bounds
	fixedRange := utils.FixRangeHeader(rangeHeader, vf.virtualFile.Size)
	if fixedRange == nil {
		return 0, vf.virtualFile.Size - 1, false
	}

	// Ensure range is valid
	if fixedRange.Start < 0 {
		fixedRange.Start = 0
	}
	if fixedRange.End >= vf.virtualFile.Size {
		fixedRange.End = vf.virtualFile.Size - 1
	}
	if fixedRange.Start > fixedRange.End {
		return 0, vf.virtualFile.Size - 1, false
	}

	return fixedRange.Start, fixedRange.End, true
}

// createUsenetReader creates a new usenet reader for the specified range
func (vf *VirtualFile) createUsenetReader(ctx context.Context, start, end int64) (io.ReadCloser, error) {
	loader := dbSegmentLoader{segs: vf.nzbFile.SegmentsData}
	// If we have a stored segment size, use it to compute ranges
	hasFixedSize := vf.nzbFile.SegmentSize > 0
	segSize := vf.nzbFile.SegmentSize

	rg := usenet.GetSegmentsInRange(start, end, loader, hasFixedSize, segSize)
	return usenet.NewUsenetReader(ctx, vf.cp, rg, vf.maxWorkers)
}
