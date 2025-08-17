package usenet

import (
	"github.com/acomagu/bufpipe"
)

type SegmentLoader interface {
	// GetSegment returns the segment with the given index.
	// If the segment is not found, it returns false.
	GetSegment(index int) (segment Segment, groups []string, ok bool)
}

func GetSegmentsInRange(
	start int64,
	end int64,
	ml SegmentLoader,
) segmentRange {
	segments := make([]*segment, 0)

	// Track the accumulative file position as we iterate through segments
	filePosition := int64(0)

	for i := 0; ; i++ {
		s, groups, ok := ml.GetSegment(i)
		if !ok {
			break
		}

		segmentSize := s.End - s.Start + 1

		// Calculate the absolute positions of this segment in the file
		segmentFileStart := filePosition + s.Start
		segmentFileEnd := filePosition + segmentSize - 1

		// Check if this segment overlaps with the requested range
		if segmentFileEnd < start || segmentFileStart > end {
			filePosition += segmentSize
			continue
		}

		r, w := bufpipe.New(nil)

		// Calculate the portion of this segment that overlaps with the requested range
		// All positions here are relative to the segment's data stream (0-based)
		segmentStart := s.Start
		segmentEnd := segmentSize - 1

		// Adjust start offset if the requested range starts after this segment begins
		if segmentFileStart < start {
			segmentStart = start - segmentFileStart
		}

		// Adjust end offset if the requested range ends before this segment ends
		if segmentFileEnd > end {
			segmentEnd = end - segmentFileStart
		}

		p := &segment{
			Id:          s.Id,
			Start:       segmentStart,
			End:         segmentEnd,
			SegmentSize: segmentSize,
			groups:      groups,
			reader:      r,
			writer:      w,
		}

		segments = append(segments, p)

		// Move to the next segment position in the file
		filePosition += segmentSize

		// If we've reached the end of the requested range, we can stop
		if segmentFileEnd >= end {
			break
		}
	}

	return segmentRange{
		segments: segments,
		start:    start,
		end:      end,
	}
}
