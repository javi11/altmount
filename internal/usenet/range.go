package usenet

import (
	"context"
	"fmt"
	"io"
)

type SegmentLoader interface {
	// GetSegment returns the segment with the given index.
	// If the segment is not found, it returns false.
	GetSegment(index int) (segment Segment, groups []string, ok bool)
}

// GetSegmentsInRange returns a segmentRange representing the requested byte range [start,end]
// across the underlying ordered segments provided by the SegmentLoader.
// Behaviour / rules:
//   - start and end are inclusive; caller guarantees 0 <= start <= end < filesize (filesize not passed here)
//   - Each loader Segment indicates:
//   - Start: offset (in bytes) within the physical segment where valid data for the file begins (can be > 0)
//   - End: offset (in bytes) within the physical segment where valid data for the file ends (inclusive)
//   - Size: full physical segment size (in bytes) for downloading
//     Therefore the usable data length contributed to the logical file by a loader segment is (End - Start + 1).
//   - We build output *segment objects (internal) with Start & End (inclusive) relative to the physical segment
//     so the reader will skip Start bytes and read up to End+1 bytes.
//   - First and last returned segments are trimmed so the concatenation of (End-Start+1) bytes across
//     returned segments equals the requested range length (unless the range lies fully outside available
//     data, in which case zero segments are returned).
func GetSegmentsInRange(ctx context.Context, start, end int64, ml SegmentLoader) *segmentRange {
	// Defensive handling of invalid input ranges
	if start < 0 || end < start {
		return &segmentRange{start: start, end: end, segments: nil, ctx: ctx}
	}

	requestedLen := end - start + 1
	segments := make([]*segment, 0, 4)

	// logicalFilePos tracks the starting file offset of the next loader segment's usable data
	var logicalFilePos int64 = 0

	for idx := 0; ; idx++ {
		src, groups, ok := ml.GetSegment(idx)
		if !ok { // no more segments
			break
		}

		// Usable data inside this segment starts at src.Start (may be >0) and ends at src.End inclusive.
		// Length contributed to file:
		usableLen := src.End - src.Start + 1
		if usableLen <= 0 { // nothing useful; skip
			continue
		}

		segFileStart := logicalFilePos             // first file offset covered by this segment's usable data
		segFileEnd := segFileStart + usableLen - 1 // last file offset covered

		// If this segment's data ends before the requested start, skip it and advance file position
		if segFileEnd < start {
			logicalFilePos += usableLen
			continue
		}
		// If this segment starts after the requested end, we are done
		if segFileStart > end {
			break
		}

		// Determine read window inside the physical segment
		// Start with full usable window (src.Start .. src.End)
		readStart := src.Start
		readEnd := src.End

		// Trim front if request starts inside this segment
		if start > segFileStart {
			// Offset (bytes) into this segment's usable data where we begin
			delta := start - segFileStart
			readStart = src.Start + delta
		}
		// Trim tail if request ends inside this segment
		if end < segFileEnd {
			delta := segFileEnd - end
			readEnd = src.End - delta
		}

		if readStart > readEnd { // safety; shouldn't happen
			logicalFilePos += usableLen
			continue
		}

		r, w := io.Pipe()
		seg := &segment{
			Id:          src.Id,
			Start:       readStart,
			End:         readEnd,
			SegmentSize: src.Size,
			groups:      groups,
			reader:      r,
			writer:      w,
		}
		segments = append(segments, seg)

		logicalFilePos += usableLen

		// If we've satisfied the full request length, stop
		// (Check by seeing if this segment covered the end)
		if segFileEnd >= end {
			break
		}

		// If we've already accumulated requestedLen bytes across segments we could also break early
		if int64AccumulatedLen(segments) >= requestedLen { // redundancy safeguard
			break
		}
	}

	return &segmentRange{segments: segments, start: start, end: end, ctx: ctx}
}

// int64AccumulatedLen calculates total bytes represented by current slice of segments
// based on (End-Start+1) for each.
func int64AccumulatedLen(segs []*segment) int64 {
	var total int64
	for _, s := range segs {
		total += (s.End - s.Start + 1)
	}
	return total
}

// CheckMetadataIntegrity verifies that the segments provided by the loader
// cover the entire file range [0, fileSize-1] without any gaps.
// Returns an error if any part of the file is not covered by segment data.
func CheckMetadataIntegrity(fileSize int64, ml SegmentLoader) error {
	if fileSize == 0 {
		return nil
	}

	var logicalFilePos int64 = 0

	for idx := 0; ; idx++ {
		src, _, ok := ml.GetSegment(idx)
		if !ok {
			break
		}

		// Gap detection: if the logical file position we're tracking
		// is less than the file position this segment would cover,
		// there's a gap in the metadata.
		// (Note: in altmount segments are expected to be perfectly sequential
		// because of how they are imported from NZBs)

		usableLen := src.End - src.Start + 1
		if usableLen <= 0 {
			continue
		}

		// We assume segments are provided in order by the loader.
		// If logicalFilePos < total size but we're out of segments or have a gap, it's corrupt.
		logicalFilePos += usableLen

		if logicalFilePos >= fileSize {
			return nil
		}
	}

	if logicalFilePos < fileSize {
		return fmt.Errorf("metadata gap: only %d of %d bytes are covered by segments", logicalFilePos, fileSize)
	}

	return nil
}

// Helper functions (avoid importing math for simple min/max & allocating in fmt for int to string)
// (helper functions removed after refactor; restore if future code requires min/max/itoa)
