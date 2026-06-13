package rar

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/parser"
)

var (
	// PartPattern matches filename.part###.rar (e.g., movie.part001.rar, movie.part01.rar)
	PartPattern = regexp.MustCompile(`^(.+)\.part(\d+)\.rar$`)
	// NumericPattern matches filename.### (numeric extensions like .001, .002)
	NumericPattern = regexp.MustCompile(`^(.+)\.(\d+)$`)
	// RPattern matches filename.r## or filename.r### (e.g., movie.r00, movie.r01)
	RPattern = regexp.MustCompile(`^(.+)\.r(\d+)$`)
	// RollVolPattern matches old-style continuation volumes whose extension letter
	// rolls after .r99 (r→s→…→z), always with two digits — e.g. movie.s00 is the
	// volume right after movie.r99. Mirrors rardecode's nextOldVolName. The leading
	// .rar is volume 0; .r00 is volume 1. Group 1 is the base, 2 the letter, 3 the digits.
	RollVolPattern       = regexp.MustCompile(`(?i)^(.+)\.([r-z])(\d{2})$`)
	partPatternNumber    = regexp.MustCompile(`\.part(\d+)\.rar$`)
	rPatternNumber       = regexp.MustCompile(`\.r(\d+)$`)
	rollVolPatternNumber = regexp.MustCompile(`(?i)\.([r-z])(\d{2})$`)
	numericPatternNumber = regexp.MustCompile(`\.(\d+)$`)
)

// SetKey returns the multi-volume grouping key for a filename and whether the
// name matches a RAR/split-volume pattern (.partNN.rar, .rar, .rNN, .NNN —
// the latter also covers .7z.NNN sets). Non-volume filenames (plain media,
// obfuscated names with no recognizable extension) return ("", false) and
// should be treated as standalone files. The key is the lowercased base name
// with the volume suffix stripped, so all parts of one set share it.
func SetKey(filename string) (string, bool) {
	lower := strings.ToLower(filepath.Base(filename))

	// Pattern 1: filename.part###.rar
	if m := PartPattern.FindStringSubmatch(lower); len(m) > 1 {
		return m[1], true
	}
	// Pattern 2: filename.rar (single part / old-style first volume)
	if before, ok := strings.CutSuffix(lower, ".rar"); ok {
		return before, true
	}
	// Pattern 3: filename.r##
	if m := RPattern.FindStringSubmatch(lower); len(m) > 1 {
		return m[1], true
	}
	// Pattern 3b: old-style rollover volumes .s##..z## (continuation after .r99)
	if m := RollVolPattern.FindStringSubmatch(lower); len(m) > 1 {
		return m[1], true
	}
	// Pattern 4: filename.### (numeric)
	if m := NumericPattern.FindStringSubmatch(lower); len(m) > 1 {
		return m[1], true
	}
	return "", false
}

// extractRarBaseName returns the lowercase base name of a RAR filename,
// stripping the part number and extension used for multi-volume sets.
// Used to group related RAR parts before archive analysis. Unlike SetKey it
// falls back to the full lowercased base name so unrecognized names still group
// only with themselves.
func extractRarBaseName(filename string) string {
	if key, ok := SetKey(filename); ok {
		return key
	}
	return strings.ToLower(filepath.Base(filename))
}

// rarScheme identifies the volume-numbering convention of a RAR/split set.
type rarScheme int

const (
	schemeUnknown rarScheme = iota
	schemePart              // name.partNN.rar (1-based)
	schemeRoll              // name.rar (vol 0) + name.rNN (vol NN+1)
	schemeNumeric           // name.NNN, e.g. .001 or .7z.001 (1-based)
)

// rarVolumeNumber extracts the volume scheme and ordinal for a filename so a set
// can be checked for contiguity. The old-style roll scheme is normalized so
// ".rar" is volume 0 and ".rNN" is volume NN+1, giving a single contiguous
// sequence. Returns ok=false for names with no recognizable volume suffix.
func rarVolumeNumber(filename string) (rarScheme, int, bool) {
	lower := strings.ToLower(filepath.Base(filename))

	// part###.rar must be checked before the plain .rar suffix.
	if m := partPatternNumber.FindStringSubmatch(lower); len(m) > 1 {
		if n := archive.ParseInt(m[1]); n >= 0 {
			return schemePart, n, true
		}
	}
	if strings.HasSuffix(lower, ".rar") {
		return schemeRoll, 0, true
	}
	// Old-style continuation volumes .r00..z99. The extension letter rolls after
	// .r99 (r→s→…→z), so the ordinal must stay contiguous across the boundary:
	// .r00=1, .r99=100, .s00=101, …, .z99=900. (.rar=0 is handled above.)
	if m := rollVolPatternNumber.FindStringSubmatch(lower); len(m) > 2 {
		letter := m[1][0]
		if nn := archive.ParseInt(m[2]); nn >= 0 {
			return schemeRoll, 1 + int(letter-'r')*100 + nn, true
		}
	}
	if m := rPatternNumber.FindStringSubmatch(lower); len(m) > 1 {
		if n := archive.ParseInt(m[1]); n >= 0 {
			return schemeRoll, n + 1, true
		}
	}
	if m := numericPatternNumber.FindStringSubmatch(lower); len(m) > 1 {
		if n := archive.ParseInt(m[1]); n >= 0 {
			return schemeNumeric, n, true
		}
	}
	return schemeUnknown, 0, false
}

// groupHasVolumeGap reports whether a RAR volume set is missing a leading or
// interior volume, judged purely from filename numbering (no network access).
// It is deliberately conservative — when the numbering scheme is mixed or any
// member is unrecognized it returns false so a healthy set is never skipped on
// a false positive. A missing *trailing* volume is undetectable by numbering
// (the expected count is unknown) and also returns false; that case is handled
// downstream by segment-integrity validation.
func groupHasVolumeGap(files []parser.ParsedFile) bool {
	if len(files) <= 1 {
		return false
	}

	scheme := schemeUnknown
	nums := make([]int, 0, len(files))
	for _, f := range files {
		s, num, ok := rarVolumeNumber(f.Filename)
		if !ok {
			return false // unrecognized member → don't risk skipping a healthy set
		}
		if scheme == schemeUnknown {
			scheme = s
		} else if s != scheme {
			return false // mixed schemes → ambiguous, don't skip
		}
		nums = append(nums, num)
	}

	sort.Ints(nums)

	expectedStart := 1
	if scheme == schemeRoll {
		expectedStart = 0
	}
	if nums[0] > expectedStart {
		return true // leading volume(s) missing
	}
	for i := 1; i < len(nums); i++ {
		if nums[i] == nums[i-1] {
			continue // duplicate ordinal (defensive); not a gap
		}
		if nums[i] != nums[i-1]+1 {
			return true // interior gap
		}
	}
	return false
}

// groupHasFirstVolume reports whether a RAR group contains the volume that begins
// an archive (the one carrying the main header): the plain .rar for the roll
// scheme, .part01.rar for the part scheme, or .001 for the numeric scheme. A group
// made only of continuation volumes (.r00.., .s00.., .002..) returns false — it
// cannot start an archive on its own.
func groupHasFirstVolume(files []parser.ParsedFile) bool {
	for _, f := range files {
		s, n, ok := rarVolumeNumber(f.Filename)
		if !ok {
			continue
		}
		start := 1
		if s == schemeRoll {
			start = 0
		}
		if n == start {
			return true
		}
	}
	return false
}

// orphanFirstVolumeName derives the name the missing first volume would have for a
// set of continuation volumes, preserving the original base name's case so
// rardecode's name-based volume following matches the real continuation files.
func orphanFirstVolumeName(orphan string, scheme rarScheme) (string, bool) {
	switch scheme {
	case schemeRoll:
		if m := RollVolPattern.FindStringSubmatch(orphan); len(m) > 1 {
			return m[1] + ".rar", true
		}
		if m := RPattern.FindStringSubmatch(orphan); len(m) > 1 {
			return m[1] + ".rar", true
		}
	case schemeNumeric:
		if m := NumericPattern.FindStringSubmatch(orphan); len(m) > 1 {
			return m[1] + ".001", true
		}
	}
	return "", false
}

// baseExtends reports whether one base name is the other extended by a "."-delimited
// suffix (or they are equal) — the signal that a reposted first volume belongs to the
// same release as its continuations (e.g. "movie.x264" and "movie.x264.repost").
func baseExtends(a, b string) bool {
	if a == b {
		return true
	}
	long, short := a, b
	if len(b) > len(a) {
		long, short = b, a
	}
	return strings.HasPrefix(long, short) && long[len(short)] == '.'
}

// reconcileRepostedFirstVolume handles a single physical RAR set whose first volume
// was reposted under a different base name (e.g. movie.repost.part01.rar) while its
// continuation volumes keep the original base (movie.r00, movie.r01, …). Grouping by
// base name splits these apart, isolating the only volume with a real archive header
// from its continuations, so only that one volume ever gets mapped.
//
// When EXACTLY ONE group is a lone first volume and every other group is pure
// continuations sharing one roll/numeric scheme, those continuations can only belong
// to that first volume. We rename the first volume to the continuations' first-volume
// name (so the whole set shares one base and rardecode follows it natively) and merge
// everything into a single group ordered by volume number. The guards are deliberately
// strict: any ambiguity (zero or multiple starters, a multi-volume starter, a
// part-scheme or unrecognized orphan, mixed orphan schemes) leaves grouping untouched.
func reconcileRepostedFirstVolume(groups [][]parser.ParsedFile) [][]parser.ParsedFile {
	if len(groups) < 2 {
		return groups
	}

	starterIdx := -1
	for i, g := range groups {
		if groupHasFirstVolume(g) {
			if starterIdx >= 0 {
				return groups // more than one group can start → genuinely separate archives
			}
			starterIdx = i
		}
	}
	if starterIdx < 0 || len(groups[starterIdx]) != 1 {
		return groups // no anchor, or the starter is itself a multi-volume set
	}

	var orphans []parser.ParsedFile
	orphanScheme := schemeUnknown
	orphanBase := ""
	for i, g := range groups {
		if i == starterIdx {
			continue
		}
		base, ok := SetKey(g[0].Filename)
		if !ok {
			return groups
		}
		if orphanBase == "" {
			orphanBase = base
		} else if base != orphanBase {
			return groups // orphans from different releases → not one set
		}
		for _, f := range g {
			s, _, ok := rarVolumeNumber(f.Filename)
			if !ok || s == schemePart {
				return groups // unrecognized or part-scheme orphan → too risky to merge
			}
			if orphanScheme == schemeUnknown {
				orphanScheme = s
			} else if s != orphanScheme {
				return groups // mixed orphan schemes → ambiguous
			}
		}
		orphans = append(orphans, g...)
	}
	if len(orphans) == 0 {
		return groups
	}

	// Only merge when the reposted first volume clearly belongs to the same release:
	// its base must be the continuations' base extended by a ".<suffix>" (e.g.
	// "movie" → "movie.repost"). Unrelated bases (a continuation set plus a different
	// single-part archive in the same NZB) are left as separate groups.
	starterBase, ok := SetKey(groups[starterIdx][0].Filename)
	if !ok || !baseExtends(starterBase, orphanBase) {
		return groups
	}

	// Order continuations by volume ordinal (contiguous across the r→s rollover).
	sort.SliceStable(orphans, func(a, b int) bool {
		_, na, _ := rarVolumeNumber(orphans[a].Filename)
		_, nb, _ := rarVolumeNumber(orphans[b].Filename)
		return na < nb
	})

	firstName, ok := orphanFirstVolumeName(orphans[0].Filename, orphanScheme)
	if !ok {
		return groups
	}

	starter := groups[starterIdx][0]
	starter.Filename = firstName
	merged := append([]parser.ParsedFile{starter}, orphans...)
	return [][]parser.ParsedFile{merged}
}

// normalizeRarPartFilename normalizes RAR part numbers while preserving padding width
// If allFilesNoExt is true, uses baseFilename for all parts with .rXX extension
// where XX is the 0-based part number (index) with zero-padding based on totalFiles
// Examples:
//   - "movie.part010.rar" -> "movie.part010.rar" (preserves padding)
//   - "movie.r00" -> "movie.r00" (preserves padding)
//   - "archive.001" -> "archive.001" (preserves padding)
//   - "movie.rar" -> "movie.rar" (no change for non-part files)
//   - Files ["abc", "def", "xyz"] with allFilesNoExt=true, baseFilename="abc", totalFiles=3:
//   - index=0 -> "abc.r00"
//   - index=1 -> "abc.r01"
//   - index=2 -> "abc.r02"
func normalizeRarPartFilename(filename string, index int, allFilesNoExt bool, totalFiles int, baseFilename string) string {
	// If all files have no extension, use baseFilename with .rXX extension
	// This ensures all parts of the same archive have the same base filename
	// Using RAR multi-volume convention: .r00, .r01, .r02, etc. (0-based)
	if allFilesNoExt && !archive.HasExtension(filename) && baseFilename != "" {
		// Calculate padding width based on total number of files (0-based, so totalFiles-1)
		width := len(strconv.Itoa(totalFiles - 1))
		// Format with zero-padding (index is already 0-based from OriginalIndex)
		paddedPartNum := fmt.Sprintf("%0*d", width, index)
		return baseFilename + ".r" + paddedPartNum
	}

	// Pattern 1: filename.part###.rar
	if matches := partPatternNumber.FindStringSubmatch(filename); len(matches) > 1 {
		partNumStr := matches[1]
		if num := archive.ParseInt(partNumStr); num >= 0 {
			// Preserve original padding width
			width := len(partNumStr)
			paddedNum := fmt.Sprintf("%0*d", width, num)
			return partPatternNumber.ReplaceAllString(filename, ".part"+paddedNum+".rar")
		}
	}

	// Pattern 2: filename.r##
	if matches := rPatternNumber.FindStringSubmatch(filename); len(matches) > 1 {
		partNumStr := matches[1]
		if num := archive.ParseInt(partNumStr); num >= 0 {
			// Preserve original padding width
			width := len(partNumStr)
			paddedNum := fmt.Sprintf("%0*d", width, num)
			return rPatternNumber.ReplaceAllString(filename, ".r"+paddedNum)
		}
	}

	// Pattern 3: filename.###
	if matches := numericPatternNumber.FindStringSubmatch(filename); len(matches) > 1 {
		partNumStr := matches[1]
		if num := archive.ParseInt(partNumStr); num >= 0 {
			// Preserve original padding width
			width := len(partNumStr)
			paddedNum := fmt.Sprintf("%0*d", width, num)
			return numericPatternNumber.ReplaceAllString(filename, "."+paddedNum)
		}
	}

	// No pattern matched, return original filename
	return filename
}
