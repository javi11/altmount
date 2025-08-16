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

	for i := 0; ; i++ {
		s, groups, ok := ml.GetSegment(i)
		if !ok {
			break
		}

		// Check if this segment overlaps with the requested range
		if s.End < start || s.Start > end {
			continue
		}

		segmentSize := s.End - s.Start + 1
		r, w := bufpipe.New(nil)
		p := &segment{
			Id:          s.Id,
			Start:       0,
			End:         segmentSize - 1,
			SegmentSize: segmentSize,
			groups:      groups,
			reader:      r,
			writer:      w,
		}

		// Adjust start offset if this is the first segment that overlaps
		if s.Start < start {
			p.Start = start - s.Start
		}

		// Adjust end offset if this is the last segment that overlaps
		if s.End > end {
			p.End = end - s.Start
		}

		segments = append(segments, p)

		// If we've reached the end of the requested range, we can stop
		if s.End >= end {
			break
		}
	}

	return segmentRange{
		segments: segments,
		start:    start,
		end:      end,
	}
}
