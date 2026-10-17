package parser

import (
	"sort"

	"github.com/kipsilabs/altmount/internal/holes"
	"github.com/javi11/nzbparser"
)

// Bounds on how much of a file may be synthesised: past a quarter of the
// segments (a handful always allowed, for short files) the numbering is more
// likely wrong than the post damaged, and the file is left as posted for the
// usual validation to judge.
const (
	maxGapFraction  = 0.25
	minGapAllowance = 4
)

func gapWithinBounds(missing, expected int) bool {
	if missing <= 0 {
		return false
	}
	return missing <= minGapAllowance || float64(missing) <= maxGapFraction*float64(expected)
}

// InsertSegmentGapPlaceholders fills holes in each file's segment numbering
// with placeholder segments (see holes.PlaceholderID). An NZB that omits an
// article leaves every later byte shifted by one segment; a placeholder of a
// typical segment's size keeps the offsets true, so archive headers still
// line up and the missing span is served as a hole instead of corrupting the
// rest of the file. The gap is known before any provider is asked, so the
// placeholders are never fetched or STAT-ed.
//
// Gaps are detected from segment numbers (1-based, contiguous when
// complete) and from the total the subject declares when it exceeds the
// highest number present — unless the release's subject totals are evidently
// made up (see subjectTotalsTrusted). Returns the number of placeholders
// inserted.
func InsertSegmentGapPlaceholders(n *nzbparser.Nzb) int {
	if n == nil {
		return 0
	}
	trustTotals := subjectTotalsTrusted(n)
	inserted := 0
	for i := range n.Files {
		inserted += fillFileGaps(&n.Files[i], trustTotals)
	}
	return inserted
}

// minTailGapEvidence is how many files must end at the same segment number
// before their disagreeing subject totals count as evidence of fake totals.
const minTailGapEvidence = 3

// subjectTotalsTrusted reports whether the "(1/N)" totals in the release's
// subjects can be believed for tail gaps. Obfuscating posters randomise N:
// every volume then ends at the same segment number (volumes share one size)
// while each subject claims a different total. A genuinely truncated post
// cannot look like that, so when several files end at one number but declare
// different totals, the totals are noise and only gaps inside the numbering
// are filled.
func subjectTotalsTrusted(n *nzbparser.Nzb) bool {
	totalsByEnd := map[int]map[int]struct{}{}
	filesByEnd := map[int]int{}
	for _, f := range n.Files {
		end, contiguous := segmentRun(f.Segments)
		if !contiguous || f.TotalSegments <= end {
			continue // complete, or a gap inside the numbering
		}
		if totalsByEnd[end] == nil {
			totalsByEnd[end] = map[int]struct{}{}
		}
		totalsByEnd[end][f.TotalSegments] = struct{}{}
		filesByEnd[end]++
	}
	for end, totals := range totalsByEnd {
		if filesByEnd[end] >= minTailGapEvidence && len(totals) > 1 {
			return false
		}
	}
	return true
}

// segmentRun returns the highest segment number and whether the numbers run
// 1..highest with nothing missing.
func segmentRun(segs nzbparser.NzbSegments) (int, bool) {
	seen := make(map[int]struct{}, len(segs))
	end := 0
	for _, s := range segs {
		if s.Number <= 0 {
			return 0, false
		}
		seen[s.Number] = struct{}{}
		end = max(end, s.Number)
	}
	return end, end > 0 && len(seen) == end
}

func fillFileGaps(f *nzbparser.NzbFile, trustTotals bool) int {
	if len(f.Segments) == 0 {
		return 0
	}
	sort.Sort(f.Segments)
	present := make(map[int]struct{}, len(f.Segments))
	maxNumber := 0
	for _, s := range f.Segments {
		if s.Number <= 0 {
			// Unnumbered segments cannot be placed; leave the file alone.
			return 0
		}
		present[s.Number] = struct{}{}
		maxNumber = max(maxNumber, s.Number)
	}
	expected := maxNumber
	if trustTotals && f.TotalSegments > expected {
		expected = f.TotalSegments
	}
	missing := expected - len(present)
	if !gapWithinBounds(missing, expected) {
		return 0
	}

	size := typicalSegmentBytes(f.Segments)
	salt := f.Segments[0].ID
	out := make(nzbparser.NzbSegments, 0, expected)
	for number := 1; number <= expected; number++ {
		if _, ok := present[number]; ok {
			continue
		}
		out = append(out, nzbparser.NzbSegment{
			Number: number,
			Bytes:  size,
			ID:     holes.PlaceholderID(number, salt),
		})
	}
	out = append(out, f.Segments...)
	sort.Sort(out)
	f.Segments = out
	return missing
}

// typicalSegmentBytes is the median declared size of a file's segments: the
// posting part size, unaffected by the shorter final article.
func typicalSegmentBytes(segs nzbparser.NzbSegments) int {
	sizes := make([]int, len(segs))
	for i, s := range segs {
		sizes[i] = s.Bytes
	}
	sort.Ints(sizes)
	return sizes[len(sizes)/2]
}
