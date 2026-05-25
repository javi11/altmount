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

// Fetcher loads a segment's decoded bytes. A return of (nil, true, nil) reports
// the article as permanently missing — a hole PAR2 must reconstruct. A non-nil
// error is transient/operational and aborts the repair so the caller can fall
// back. The engine calls this once per segment (present and missing alike) so it
// can classify holes itself without a separate pre-fetch cache.
type Fetcher interface {
	Fetch(ctx context.Context, messageID string) (data []byte, missing bool, err error)
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
//   - fetch supplies each segment's decoded bytes and reports holes. The engine
//     classifies holes itself (a permanently-missing or wrong-sized article),
//     so the caller does not pre-compute the missing set or pre-fetch bodies.
//
// Memory: the only full-file-sized allocation is the PAR2 slice grid. Present
// segments scatter straight into it and their raw article bytes are released
// before the next fetch, so peak usage stays ~1× the file size — not the ~2× a
// separate fetched-bytes cache plus the grid would cost. Reed-Solomon recovery
// inherently needs every surviving slice, so the whole file (minus holes) is
// still read; this only bounds how much is resident at once.
//
// It returns which segments were recovered, or an error (e.g. insufficient
// recovery blocks, in which case the caller should fall back to a full
// re-download). When nothing is actually missing it returns an empty result so
// the caller can simply retry the read.
func RepairFileSegments(
	ctx context.Context,
	rs *RecoverySet,
	segments []Segment,
	fetch Fetcher,
	sink Sink,
) (*RepairResult, error) {
	layout, total := rs.Layout()
	if len(layout) != 1 {
		return nil, ErrMultiFileUnsupported
	}
	ss := int(rs.SliceSize)
	if ss <= 0 {
		return nil, fmt.Errorf("par2: invalid slice size %d", rs.SliceSize)
	}
	fileID := layout[0].ID
	fileLen := int64(rs.Files[fileID].Length)

	// The slice grid is the repair's single full-file allocation. We can't know
	// which slices are holes until the fetch pass completes, so allocate the
	// whole grid up front; slices that turn out to overlap a hole are released
	// (set nil) before reconstruction.
	present := make([][]byte, total)
	for s := range present {
		present[s] = make([]byte, ss) // zero-padded by construction
	}

	// Single streaming pass: fetch every segment once, scatter present bodies
	// into the grid, and record holes. A permanently-missing or wrong-sized
	// article is a hole that PAR2 reconstructs below; its (absent or garbage)
	// bytes are never retained.
	missing := make(map[string]bool)
	for _, seg := range segments {
		data, miss, err := fetch.Fetch(ctx, seg.MessageID)
		if err != nil {
			return nil, fmt.Errorf("par2: fetch segment %s: %w", seg.MessageID, err)
		}
		if miss || int64(len(data)) != seg.End-seg.Start {
			missing[seg.MessageID] = true
			continue
		}
		if err := scatter(present, ss, total, seg, data); err != nil {
			return nil, err
		}
		// data is loop-scoped and unreferenced after this point, so it's eligible
		// for collection before the next fetch — peak stays ~1× the file.
	}
	if len(missing) == 0 {
		return &RepairResult{}, nil
	}

	// A slice that overlaps any hole cannot be trusted (a present segment may
	// share it with a missing one); drop its partial bytes so Reconstruct
	// rebuilds it wholesale.
	sliceMissing, _ := markMissingSlices(rs, segments, missing)
	fixed := make([]int, 0)
	for s, m := range sliceMissing {
		if m {
			present[s] = nil
			fixed = append(fixed, s)
		}
	}

	// Reed-Solomon: recover the missing slices.
	out, err := rs.Reconstruct(present)
	if err != nil {
		return nil, err
	}

	// Verify the reconstructed slices against the PAR2 IFSC checksums before
	// emitting anything. If any assumption broke (offset convention, ordering,
	// padding, a foreign recovery set) the math can still "succeed" yet produce
	// garbage; serving it would be worse than the ARR fallback because we'd
	// silently cache and stream corruption. On mismatch, fail so the caller
	// re-downloads.
	if bad := rs.verifySlices(fileID, out, fixed); bad >= 0 {
		return nil, fmt.Errorf("%w: slice %d", ErrVerificationFailed, bad)
	}

	// Extract each missing segment's exact byte range from the reconstructed
	// stream and hand it to the sink.
	result := &RepairResult{SlicesFixed: len(fixed)}
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

// scatter copies a present segment's bytes into the slice-grid positions it
// covers. It is only called for full-size articles, so the bounds guard is
// defense-in-depth against a Fetcher that breaks that contract: the scatter
// runs in a background goroutine with no recover(), where a slice-bounds panic
// would crash the process. Fail with a diagnosable error instead.
func scatter(present [][]byte, ss, total int, seg Segment, data []byte) error {
	for off := seg.Start; off < seg.End; {
		s := int(off / int64(ss))
		within := int(off % int64(ss))
		n := ss - within
		if int64(n) > seg.End-off {
			n = int(seg.End - off)
		}
		if s >= 0 && s < total && present[s] != nil {
			srcOff := off - seg.Start
			if srcOff < 0 || srcOff+int64(n) > int64(len(data)) {
				return fmt.Errorf("par2: segment %s shorter than its declared range (have %d bytes)", seg.MessageID, len(data))
			}
			copy(present[s][within:within+n], data[srcOff:srcOff+int64(n)])
		}
		off += int64(n)
	}
	return nil
}

// markMissingSlices returns, per global input-block index, whether the slice
// overlaps any missing segment (and so must be reconstructed), plus the total
// slice count from the recovery-set layout. A slice that any missing segment
// touches is unrecoverable from present data and is marked missing wholesale.
func markMissingSlices(rs *RecoverySet, segments []Segment, missing map[string]bool) (sliceMissing []bool, total int) {
	_, total = rs.Layout()
	sliceMissing = make([]bool, total)
	ss := int64(rs.SliceSize)
	if ss <= 0 {
		return sliceMissing, total
	}
	for _, seg := range segments {
		if !missing[seg.MessageID] {
			continue
		}
		first := int(seg.Start / ss)
		last := int((seg.End - 1) / ss)
		for s := first; s <= last && s < total; s++ {
			if s >= 0 {
				sliceMissing[s] = true
			}
		}
	}
	return sliceMissing, total
}

// MissingSlices reports how many PAR2 input slices are affected by the given
// missing segments, and the recovery set's total slice count. It is a cheap,
// allocation-light way to decide whether reconstruction is even possible
// (recovery-slice count >= missing-slice count) before committing to the
// whole-file fetch that reconstruction requires.
func MissingSlices(rs *RecoverySet, segments []Segment, missing map[string]bool) (missingCount, total int) {
	sliceMissing, total := markMissingSlices(rs, segments, missing)
	for _, m := range sliceMissing {
		if m {
			missingCount++
		}
	}
	return missingCount, total
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
