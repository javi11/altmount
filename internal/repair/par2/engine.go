package par2

import (
	"context"
	"errors"
	"fmt"
)

// ErrMultiFileUnsupported is returned when a recovery set spans more than one
// file. Reconstructing such a set requires the data of every file in the set,
// which a single virtual file's metadata cannot supply; this is a documented
// future extension.
var ErrMultiFileUnsupported = errors.New("par2: multi-file recovery sets are not yet supported")

// Segment is one NZB article's contribution to a file's decoded byte stream.
// Start is inclusive, End exclusive. Segments are expected to tile the file
// contiguously in order.
type Segment struct {
	MessageID string
	Start     int64
	End       int64
}

// Fetcher loads the decoded bytes of a present (downloadable) segment.
type Fetcher interface {
	Fetch(ctx context.Context, messageID string) ([]byte, error)
}

// Sink receives the reconstructed decoded bytes of a previously-missing
// segment, keyed by its message-ID (e.g. the segment cache).
type Sink interface {
	Put(messageID string, data []byte) error
}

// RepairResult summarises a repair attempt.
type RepairResult struct {
	Recovered   []string // message-IDs successfully reconstructed and sunk
	SlicesFixed int      // number of PAR2 input slices reconstructed
}

// RepairFileSegments reconstructs the missing segments of a single-file PAR2
// recovery set and writes their decoded bytes to sink.
//
//   - segments must be the complete, ordered, contiguous segment layout of the
//     file (byte offsets within the decoded file).
//   - missing is the set of message-IDs that could not be downloaded.
//   - fetch supplies the decoded bytes of present segments (needed to rebuild
//     the surviving PAR2 slices — Reed-Solomon recovery requires all surviving
//     data, so the whole file minus the holes is read).
//
// It returns which segments were recovered, or an error (e.g. insufficient
// recovery blocks, in which case the caller should fall back to a full
// re-download).
func RepairFileSegments(
	ctx context.Context,
	rs *RecoverySet,
	segments []Segment,
	missing map[string]bool,
	fetch Fetcher,
	sink Sink,
) (*RepairResult, error) {
	layout, total := rs.Layout()
	if len(layout) != 1 {
		return nil, ErrMultiFileUnsupported
	}
	if len(missing) == 0 {
		return &RepairResult{}, nil
	}
	ss := int(rs.SliceSize)
	fileLen := int64(rs.Files[layout[0].ID].Length)

	// Decide, per PAR2 slice, whether it is fully covered by present segments.
	// A slice that overlaps any missing segment cannot be assembled and must be
	// reconstructed.
	sliceMissing := make([]bool, total)
	for _, seg := range segments {
		if !missing[seg.MessageID] {
			continue
		}
		first := int(seg.Start / int64(ss))
		last := int((seg.End - 1) / int64(ss))
		for s := first; s <= last && s < total; s++ {
			if s >= 0 {
				sliceMissing[s] = true
			}
		}
	}

	// Assemble the surviving slices from present segments. Fetch each present
	// segment once.
	present := make([][]byte, total)
	for s := 0; s < total; s++ {
		if sliceMissing[s] {
			continue // leave nil → to be reconstructed
		}
		present[s] = make([]byte, ss) // zero-padded by construction
	}
	fetched := make(map[string][]byte)
	for _, seg := range segments {
		if missing[seg.MessageID] {
			continue
		}
		// Which slices does this segment feed?
		first := int(seg.Start / int64(ss))
		last := int((seg.End - 1) / int64(ss))
		needed := false
		for s := first; s <= last && s < total; s++ {
			if s >= 0 && !sliceMissing[s] {
				needed = true
				break
			}
		}
		if !needed {
			continue
		}
		data, ok := fetched[seg.MessageID]
		if !ok {
			d, err := fetch.Fetch(ctx, seg.MessageID)
			if err != nil {
				return nil, fmt.Errorf("par2: fetch present segment %s: %w", seg.MessageID, err)
			}
			fetched[seg.MessageID] = d
			data = d
		}
		// Scatter the segment's bytes into the present slices it covers.
		for off := seg.Start; off < seg.End; {
			s := int(off / int64(ss))
			within := int(off % int64(ss))
			n := ss - within
			if int64(n) > seg.End-off {
				n = int(seg.End - off)
			}
			if s >= 0 && s < total && present[s] != nil {
				srcOff := off - seg.Start
				copy(present[s][within:within+n], data[srcOff:srcOff+int64(n)])
			}
			off += int64(n)
		}
	}

	// Reed-Solomon: recover the missing slices.
	out, err := rs.Reconstruct(present)
	if err != nil {
		return nil, err
	}

	// Extract each missing segment's exact byte range from the reconstructed
	// stream and hand it to the sink.
	result := &RepairResult{}
	for s := range sliceMissing {
		if sliceMissing[s] {
			result.SlicesFixed++
		}
	}
	for _, seg := range segments {
		if !missing[seg.MessageID] {
			continue
		}
		end := seg.End
		if end > fileLen {
			end = fileLen // never emit padding past EOF
		}
		payload := readRange(out, ss, seg.Start, end)
		if err := sink.Put(seg.MessageID, payload); err != nil {
			return nil, fmt.Errorf("par2: sink missing segment %s: %w", seg.MessageID, err)
		}
		result.Recovered = append(result.Recovered, seg.MessageID)
	}
	return result, nil
}

// readRange concatenates bytes [start,end) from slice-aligned blocks.
func readRange(slices [][]byte, ss int, start, end int64) []byte {
	out := make([]byte, end-start)
	for off := start; off < end; {
		s := int(off / int64(ss))
		within := int(off % int64(ss))
		n := ss - within
		if int64(n) > end-off {
			n = int(end - off)
		}
		copy(out[off-start:], slices[s][within:within+n])
		off += int64(n)
	}
	return out
}
