// Package nzbgap detects and fills segment gaps in parsed NZBs: segment
// numbers the NZB simply does not list. Such articles were never indexed, so
// they have no message ID to fetch or STAT — but their byte positions are
// derivable from the numbers around them, which is all PAR2 repair needs.
//
// FillFile inserts a deterministic synthetic placeholder segment per missing
// number. Downstream, placeholders behave exactly like dead articles: STAT
// and fetch of a synthetic ID can only fail (the ID is ours, no provider has
// it), the fast-fail sweep counts the file broken, PAR2 repair plans the
// covered slices as missing, and the patch it emits is stored under the
// synthetic ID — where the read path's patch-before-fetch lookup finds it.
//
// Determinism matters: the importer and the NZB-mode repair engine each parse
// the NZB independently, and the patch written by one must be found by the
// other. The ID is derived from the file's first listed segment ID and the
// missing number, both stable properties of the NZB itself.
package nzbgap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/javi11/nzbparser"
)

// syntheticDomain marks placeholder IDs. The .invalid TLD is reserved
// (RFC 2606), so no real article can carry it.
const syntheticDomain = "@gap.altmount.invalid"

// SyntheticID returns the deterministic placeholder message ID for the
// missing segment number of the file anchored by anchorID (the file's first
// listed segment ID).
func SyntheticID(anchorID string, number int) string {
	sum := sha256.Sum256([]byte(anchorID))
	return fmt.Sprintf("gap-%d-%s%s", number, hex.EncodeToString(sum[:8]), syntheticDomain)
}

// IsSynthetic reports whether id is a placeholder minted by SyntheticID.
func IsSynthetic(id string) bool {
	return strings.HasSuffix(strings.TrimSuffix(strings.TrimPrefix(id, "<"), ">"), syntheticDomain)
}

// FillFile inserts placeholder segments for every segment number missing from
// f, keeping the segment list sorted by number, and returns the inserted IDs.
// The expected count is the highest number listed, or f.TotalSegments when
// that is larger (tail gaps). Placeholders carry Bytes 0: every consumer that
// needs real sizes re-derives them (yEnc normalization on import, FileDesc
// lengths in repair). Idempotent — numbers already present, synthetic or not,
// are left alone.
func FillFile(f *nzbparser.NzbFile) []string {
	if len(f.Segments) == 0 {
		return nil
	}
	present := make(map[int]bool, len(f.Segments))
	maxNum := 0
	anchor := ""
	minNumber := f.Segments[0].Number
	for _, s := range f.Segments {
		present[s.Number] = true
		if s.Number > maxNum {
			maxNum = s.Number
		}
		if s.Number < minNumber || anchor == "" {
			minNumber = s.Number
			anchor = s.ID
		}
	}
	if f.TotalSegments > maxNum {
		maxNum = f.TotalSegments
	}

	var inserted []string
	for n := 1; n <= maxNum; n++ {
		if present[n] {
			continue
		}
		id := SyntheticID(anchor, n)
		f.Segments = append(f.Segments, nzbparser.NzbSegment{Number: n, ID: id})
		inserted = append(inserted, id)
	}
	if len(inserted) > 0 {
		sort.Slice(f.Segments, func(i, j int) bool { return f.Segments[i].Number < f.Segments[j].Number })
	}
	return inserted
}

// Fill runs FillFile over every file of the NZB and returns the synthetic IDs
// inserted, keyed by filename. Files without gaps are absent from the map.
func Fill(n *nzbparser.Nzb) map[string][]string {
	gaps := map[string][]string{}
	for i := range n.Files {
		if ids := FillFile(&n.Files[i]); len(ids) > 0 {
			gaps[fileKey(n.Files[i])] = ids
		}
	}
	return gaps
}

// fileKey names a file for Fill's report: the parsed filename when the parser
// recovered one, else the subject.
func fileKey(f nzbparser.NzbFile) string {
	if f.Filename != "" {
		return f.Filename
	}
	return f.Subject
}
