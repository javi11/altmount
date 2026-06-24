package rar

import (
	"fmt"
	"sort"

	"github.com/javi11/altmount/internal/importer/parser"
)

// obfuscatedSetMinVolumes is the floor below which reconcileObfuscatedPartSet
// will not act. Genuine obfuscated multi-volume releases have many parts;
// requiring at least this many keeps the heuristic clear of small, ambiguous
// groupings.
const obfuscatedSetMinVolumes = 3

// obfuscatedSetSyntheticBase is the base name every volume is renamed to when an
// obfuscated part-set is detected. It MUST contain no digits: rardecode derives
// each next volume name by incrementing the digit run it finds in the current
// name (see nextNewVolName in the rardecode library), so any digit in the base
// would be incremented instead of the part number and break volume following.
// A constant is safe because detection only fires when the entire NZB is a
// single obfuscated set, so there is exactly one set to name.
const obfuscatedSetSyntheticBase = "obfuscatedrarset"

// reconcileObfuscatedPartSet detects an NZB that is a single multi-volume RAR
// set whose every volume carries a different random base name (e.g.
// aB3x.part01.rar, Q4I6.part02.rar, …) with only the ".partNN.rar" tail shared.
//
// Grouping by base name turns such a set into N single-file groups, which
// defeats analysis: rardecode follows a multi-volume set by computing sibling
// names from the first volume's name, so randomized bases make the chain
// unfollowable. Each volume analyzed alone yields nothing for continuation
// volumes (their header has no first-block to anchor a file) and only the true
// first volume produces a Content — one declaring the full file size while
// carrying only its own ~1/N of the segments. The import then looks healthy but
// is truncated, and any read past the first volume fails.
//
// When the grouping matches that exact shape, this folds the volumes back into
// one group and renames them onto a single shared, digit-free base in part
// order, so rardecode's native volume following works and the standard
// multi-volume path assembles the complete file. Renaming only changes each
// file's Filename; segments, NzbdavID and all other fields travel unchanged, and
// the final output name comes from the archive's internal file name, not these
// volume names.
//
// The predicate is deliberately strict: it acts only when EVERY group is a
// single-file, part-scheme, distinctly-based volume and their ordinals form a
// contiguous 1..N run with no gaps or duplicates. Anything ambiguous — a clean
// shared-base set present alongside (which never groups into singletons), a
// duplicate ordinal from two overlapping sets, a missing interior volume, a
// non-part scheme — leaves the grouping untouched. An import that works today
// can never be altered, and an ambiguous one is left exactly as-is rather than
// guessed at. (A missing *trailing* volume past N is undetectable from numbering
// and is left to downstream segment-coverage validation, as it is today.)
func reconcileObfuscatedPartSet(groups [][]parser.ParsedFile) [][]parser.ParsedFile {
	if len(groups) < obfuscatedSetMinVolumes {
		return groups
	}

	type vol struct {
		ordinal int
		file    parser.ParsedFile
	}
	vols := make([]vol, 0, len(groups))
	seenOrdinal := make(map[int]struct{}, len(groups))
	seenBase := make(map[string]struct{}, len(groups))

	for _, g := range groups {
		// Every group must be a single volume. A multi-file group means a normal
		// shared-base set is present (clean, or a clean set mixed in), which must
		// not be disturbed.
		if len(g) != 1 {
			return groups
		}
		f := g[0]

		// Must be a part-scheme volume (.partNN.rar).
		scheme, ordinal, ok := rarVolumeNumber(f.Filename)
		if !ok || scheme != schemePart {
			return groups
		}

		// A distinct base per volume is the signature of per-volume obfuscation.
		// A shared base would not have produced single-file groups; guard anyway.
		base, ok := SetKey(f.Filename)
		if !ok {
			return groups
		}
		if _, dup := seenBase[base]; dup {
			return groups
		}
		seenBase[base] = struct{}{}

		// A duplicate ordinal means two overlapping sets (e.g. both start at
		// part01). Unresolvable from names alone; bail rather than splice volumes
		// from different files together.
		if _, dup := seenOrdinal[ordinal]; dup {
			return groups
		}
		seenOrdinal[ordinal] = struct{}{}

		vols = append(vols, vol{ordinal: ordinal, file: f})
	}

	// Ordinals must be exactly the contiguous run 1..N (N == number of groups).
	// With N distinct ordinals already guaranteed, requiring each of 1..N to be
	// present forces the set to be exactly {1..N} (no leading/interior gap, no
	// out-of-range value).
	n := len(vols)
	for i := 1; i <= n; i++ {
		if _, ok := seenOrdinal[i]; !ok {
			return groups
		}
	}

	// Matched. Order by true part number and rename onto one digit-free base so
	// rardecode follows the chain natively. Padding width tracks N so part
	// numbers are zero-padded consistently (part01..part63 for N=63).
	sort.Slice(vols, func(a, b int) bool { return vols[a].ordinal < vols[b].ordinal })
	width := len(fmt.Sprint(n))

	merged := make([]parser.ParsedFile, 0, n)
	for _, v := range vols {
		f := v.file // struct copy; preserves Segments, NzbdavID, OriginalIndex, etc.
		f.Filename = fmt.Sprintf("%s.part%0*d.rar", obfuscatedSetSyntheticBase, width, v.ordinal)
		merged = append(merged, f)
	}
	return [][]parser.ParsedFile{merged}
}
