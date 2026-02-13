package vfs

import "sort"

// Range represents a contiguous byte range [Start, End).
type Range struct {
	Start int64
	End   int64
}

// Ranges maintains a sorted, non-overlapping, coalesced list of byte ranges.
// It tracks which byte ranges have been downloaded/cached on disk.
type Ranges struct {
	items []Range
}

// NewRanges creates an empty Ranges.
func NewRanges() *Ranges {
	return &Ranges{}
}

// Insert adds a byte range and coalesces overlapping/adjacent ranges.
func (r *Ranges) Insert(start, end int64) {
	if start >= end {
		return
	}

	newRange := Range{Start: start, End: end}

	if len(r.items) == 0 {
		r.items = append(r.items, newRange)
		return
	}

	// Find insertion point using binary search
	i := sort.Search(len(r.items), func(j int) bool {
		return r.items[j].End >= start
	})

	// Find last overlapping/adjacent range
	j := i
	for j < len(r.items) && r.items[j].Start <= end {
		if r.items[j].Start < newRange.Start {
			newRange.Start = r.items[j].Start
		}
		if r.items[j].End > newRange.End {
			newRange.End = r.items[j].End
		}
		j++
	}

	// Replace overlapping ranges with merged range
	merged := make([]Range, 0, len(r.items)-(j-i)+1)
	merged = append(merged, r.items[:i]...)
	merged = append(merged, newRange)
	merged = append(merged, r.items[j:]...)
	r.items = merged
}

// Present checks if the entire range [start, end) is covered.
func (r *Ranges) Present(start, end int64) bool {
	if start >= end {
		return true
	}

	i := sort.Search(len(r.items), func(j int) bool {
		return r.items[j].End > start
	})

	if i >= len(r.items) {
		return false
	}

	return r.items[i].Start <= start && r.items[i].End >= end
}

// FindMissing returns the byte ranges within [start, end) that are NOT present.
func (r *Ranges) FindMissing(start, end int64) []Range {
	if start >= end {
		return nil
	}

	if len(r.items) == 0 {
		return []Range{{Start: start, End: end}}
	}

	var missing []Range
	pos := start

	i := sort.Search(len(r.items), func(j int) bool {
		return r.items[j].End > start
	})

	for ; i < len(r.items) && pos < end; i++ {
		item := r.items[i]
		if item.Start > pos {
			gapEnd := min(item.Start, end)
			missing = append(missing, Range{Start: pos, End: gapEnd})
		}
		if item.End > pos {
			pos = item.End
		}
	}

	if pos < end {
		missing = append(missing, Range{Start: pos, End: end})
	}

	return missing
}

// Size returns the total number of bytes covered by all ranges.
func (r *Ranges) Size() int64 {
	var total int64
	for _, item := range r.items {
		total += item.End - item.Start
	}
	return total
}

// Count returns the number of non-overlapping ranges.
func (r *Ranges) Count() int {
	return len(r.items)
}

// Items returns a copy of the internal ranges for serialization.
func (r *Ranges) Items() []Range {
	out := make([]Range, len(r.items))
	copy(out, r.items)
	return out
}

// FromItems rebuilds ranges from a previously serialized slice.
func (r *Ranges) FromItems(items []Range) {
	r.items = make([]Range, len(items))
	copy(r.items, items)
}
