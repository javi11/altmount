package usenet

import (
	"fmt"
	"strconv"

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

	// Track coverage within requested range
	var totalCovered int64

	for i := 0; ; i++ {
		s, groups, ok := ml.GetSegment(i)
		if !ok {
			break
		}

		segmentSize := s.Size

		// Calculate the absolute positions of this segment in the file
		segmentFileStart := filePosition + s.Start
		segmentFileEnd := filePosition + segmentSize

		// Check if this segment overlaps with the requested range
		if segmentFileEnd < start {
			filePosition += segmentSize
			continue
		}

		r, w := bufpipe.New(nil)

		// Calculate the portion of this segment that overlaps with the requested range
		// All positions here are relative to the segment's data stream (0-based)
		segmentStart := s.Start

		// Adjust start offset if the requested range starts after this segment begins
		if segmentFileStart < start {
			segmentStart = start - segmentFileStart
		}

		p := &segment{
			Id:          s.Id,
			Start:       segmentStart,
			SegmentSize: segmentSize,
			End:         segmentSize - 1,
			groups:      groups,
			reader:      r,
			writer:      w,
		}

		segments = append(segments, p)

		totalCovered += segmentSize - (segmentStart + 1)
		// Move to the next segment position in the file
		filePosition += segmentSize

		// If we've reached the end of the requested range, we can stop
		if segmentFileEnd >= end {
			p.End = segmentSize - (segmentFileEnd - end)
			break
		}
	}

	requestedSize := end - start + 1
	diff := requestedSize - totalCovered

	fmt.Printf("GetSegmentsInRange summary requested_start=%d requested_end=%d requested_size=%d segments_count=%d total_covered=%d size_difference=%d\n",
		start,
		end,
		requestedSize,
		len(segments),
		totalCovered,
		diff)

	return segmentRange{
		segments: segments,
		start:    start,
		end:      end,
	}
}

// Helper functions (avoid importing math for simple min/max & allocating in fmt for int to string)
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func itoa(i int64) string { // minimal allocation integer to string
	return strconv.FormatInt(i, 10)
}
