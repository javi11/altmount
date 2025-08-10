package usenet

import (
	"github.com/acomagu/bufpipe"
	"github.com/javi11/nzbparser"
)

type SegmentLoader interface {
	// GetSegment returns the segment with the given index.
	// If the segment is not found, it returns false.
	GetSegment(index int) (segment nzbparser.NzbSegment, groups []string, ok bool)
}

func GetSegmentsInRange(
	start int64,
	end int64,
	ml SegmentLoader,
	hasWrongSizes bool,
	segmentSize int64,
) segmentRange {
	size := 0
	segments := make([]*segment, 0)

	for i := 0; ; i++ {
		s, groups, ok := ml.GetSegment(i)
		if !ok {
			break
		}

		// If segmentSize is provided use it since it will be more reliable than the segment size from the nzb.
		sSize := s.Bytes
		if hasWrongSizes {
			sSize = int(segmentSize)
		}

		size += sSize
		if size < int(start) {
			continue
		}

		r, w := bufpipe.New(nil)
		p := &segment{
			Id:          s.ID,
			Start:       0,
			End:         int64(sSize - 1),
			SegmentSize: int64(sSize),
			groups:      groups,
			reader:      r,
			writer:      w,
		}

		// Handles the first segment within the range.
		if len(segments) == 0 {
			p.Start = start - int64(size-sSize)
		}

		segments = append(segments, p)

		// Handles the last segment within the range.
		if size >= int(end) {
			p.End = int64(s.Bytes) - (int64(size) - end)
			break
		}
	}

	return segmentRange{
		segments: segments,
		start:    start,
		end:      end,
	}
}
