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
	// Re-exported from the archive package, which is the single source of truth for
	// these patterns (the filesystem and sevenzip packages depend on them too).
	PartPattern    = archive.PartPattern
	NumericPattern = archive.NumericPattern
	RPattern       = archive.RPattern
	RollVolPattern = archive.RollVolPattern

	// Number-only patterns used locally by normalizeRarPartFilename to rewrite a
	// volume's digits while preserving its padding width.
	partPatternNumber    = regexp.MustCompile(`\.part(\d+)\.rar$`)
	rPatternNumber       = regexp.MustCompile(`\.r(\d+)$`)
	numericPatternNumber = regexp.MustCompile(`\.(\d+)$`)
)

// SetKey returns the multi-volume grouping key for a filename and whether the
// name matches a RAR/split-volume pattern (.partNN.rar, .rar, .rNN, .NNN —
// the latter also covers .7z.NNN sets). Non-volume filenames (plain media,
// obfuscated names with no recognizable extension) return ("", false) and
// should be treated as standalone files. The key is the lowercased base name
// with the volume suffix stripped, so all parts of one set share it.
//
// Filenames must already be canonical; poster-added quotes are stripped upstream at
// the nzbparser boundary (parser.SanitizeNzbFilenames), so SetKey does no cleaning.
func SetKey(filename string) (string, bool) {
	return archive.SetKey(filename)
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

// volumeNumberKey derives a width-independent (set, scheme, volume-number) key for
// a RAR volume filename, or ("", false) when the name is not a recognized volume.
// It lets a part be matched regardless of zero-padding width — rardecode follows a
// set by computing fixed-width names (e.g. ...part10.rar) while the source files
// may be padded differently (...part010.rar).
func volumeNumberKey(filename string) (string, bool) {
	setKey, ok := SetKey(filename)
	if !ok {
		return "", false
	}
	scheme, num, ok := rarVolumeNumber(filename)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s\x00%d\x00%d", setKey, int(scheme), num), true
}

// partLocator resolves a rardecode part.Path to a previously-registered value,
// tolerating volume-name width mismatches. Exact filename (full then base) is tried
// first; on a miss it falls back to the width-independent volume-number key. The
// same defect — rardecode emitting "part10.rar" when the real volume is
// "part010.rar" — breaks both volume following (handled in the filesystem layer)
// and the part→source mapping here, so both need this fallback.
type partLocator[T any] struct {
	byName   map[string]T
	byNumber map[string]T
}

func newPartLocator[T any](capacity int) partLocator[T] {
	return partLocator[T]{
		byName:   make(map[string]T, capacity*2),
		byNumber: make(map[string]T, capacity),
	}
}

// add registers a source value under its filename. The first entry for a given
// volume-number key wins (exact-name matches are unaffected).
func (l partLocator[T]) add(filename string, v T) {
	l.byName[filename] = v
	l.byName[filepath.Base(filename)] = v
	if key, ok := volumeNumberKey(filename); ok {
		if _, exists := l.byNumber[key]; !exists {
			l.byNumber[key] = v
		}
	}
}

// get resolves a part path, returning the registered value and whether it matched.
func (l partLocator[T]) get(partPath string) (T, bool) {
	if v, ok := l.byName[partPath]; ok {
		return v, true
	}
	if v, ok := l.byName[filepath.Base(partPath)]; ok {
		return v, true
	}
	if key, ok := volumeNumberKey(partPath); ok {
		if v, ok := l.byNumber[key]; ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// rarScheme and the scheme constants alias the canonical definitions in the
// archive package so existing rar-package code keeps compiling unchanged.
type rarScheme = archive.RarScheme

const (
	schemeUnknown = archive.SchemeUnknown
	schemePart    = archive.SchemePart
	schemeRoll    = archive.SchemeRoll
	schemeNumeric = archive.SchemeNumeric
)

// rarVolumeNumber extracts the volume scheme and ordinal for a filename so a set
// can be checked for contiguity. Delegates to archive.VolumeNumber.
func rarVolumeNumber(filename string) (rarScheme, int, bool) {
	return archive.VolumeNumber(filename)
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

// canonicalVolumeName builds the canonical multi-volume filename for a (scheme,
// ordinal) under a shared base, matching rarname.VolumeNumber's numbering so the
// rewritten names round-trip and rardecode follows them by computed name. The
// ordinal is the value VolumeNumber returns:
//   - roll: 0 -> .rar, 1 -> .r00, 2 -> .r01, … 100 -> .r99, 101 -> .s00 …
//   - part: 1 -> .part01.rar, 2 -> .part02.rar …
//   - numeric: 1 -> .001, 2 -> .002 …
//
// The shared base lets a distinct-base obfuscated set be rewritten so rardecode's
// name-based volume following reaches every part.
func canonicalVolumeName(base string, scheme rarScheme, num int) string {
	switch scheme {
	case schemeRoll:
		if num <= 0 {
			return base + ".rar"
		}
		k := num - 1 // .r00 is ordinal 1
		letter := byte('r') + byte(k/100)
		return fmt.Sprintf("%s.%c%02d", base, letter, k%100)
	case schemePart:
		return fmt.Sprintf("%s.part%02d.rar", base, num)
	case schemeNumeric:
		return fmt.Sprintf("%s.%03d", base, num)
	default:
		return base
	}
}

// obfuscatedUnifiedBase is the synthetic base name applied to a reassembled
// obfuscated volume set. It is used only internally for rardecode's volume
// following and the part→segment lookup; the imported file's virtual path comes
// from the inner archive's own filename, so this name never surfaces to the user.
const obfuscatedUnifiedBase = "obfuscated_volume_set"

// reconcileObfuscatedVolumeSet folds a single multi-volume RAR set whose every
// volume carries a DISTINCT obfuscated base name (e.g. abc.xyz.<hash1>.r00,
// abc.xyz.<hash2>.r01, …) back into one ordered group. Grouping by base name shatters
// such a set into one single-file group per volume, so only the header-carrying volume
// ever analyzes — yielding a file whose declared size dwarfs its mapped segments
// (the "metadata gap" failure). With no PAR2 to recover a shared base, the reconstruction
// relies on the volumes' own contiguous ordinals plus an optional headerless first volume.
//
// The guards are deliberately strict so genuinely separate archives are never merged:
//   - every group must be a single file (the per-volume-obfuscation fingerprint; any
//     shared base leaves the normal/reposted-volume paths to handle it),
//   - at least two volumes carry recognizable ordinals in ONE scheme, contiguous and
//     starting at that scheme's expected first continuation,
//   - at most one file is unrecognized (the headerless first volume of a roll/numeric set).
//
// On a match it returns a single group, renamed to a shared synthetic base and ordered
// by volume number, so rardecode follows the whole sequence. Any ambiguity returns the
// groups untouched; the segment-coverage guard then fails such an import loudly instead.
func reconcileObfuscatedVolumeSet(groups [][]parser.ParsedFile) [][]parser.ParsedFile {
	if len(groups) < 3 {
		return groups
	}

	files := make([]parser.ParsedFile, 0, len(groups))
	for _, g := range groups {
		if len(g) != 1 {
			return groups // a shared base grouped >1 volume → not the distinct-base case
		}
		files = append(files, g[0])
	}

	type vol struct {
		file parser.ParsedFile
		num  int
	}
	scheme := schemeUnknown
	var recognized []vol
	var starters []parser.ParsedFile
	for _, f := range files {
		s, num, ok := rarVolumeNumber(f.Filename)
		if !ok {
			starters = append(starters, f)
			continue
		}
		if scheme == schemeUnknown {
			scheme = s
		} else if s != scheme {
			return groups // mixed schemes → ambiguous
		}
		recognized = append(recognized, vol{file: f, num: num})
	}
	if scheme == schemeUnknown || len(recognized) < 2 || len(starters) > 1 {
		return groups
	}

	sort.Slice(recognized, func(a, b int) bool { return recognized[a].num < recognized[b].num })
	for i := 1; i < len(recognized); i++ {
		if recognized[i].num <= recognized[i-1].num || recognized[i].num != recognized[i-1].num+1 {
			return groups // duplicate or gap → not a clean single set
		}
	}

	// rarname.VolumeNumber ordinals: roll .rar=0/.r00=1/.r01=2…, part .part01.rar=1…,
	// numeric .001=1…. So a roll set's continuations (.r00..) begin at ordinal 1 and its
	// headerless first volume is ordinal 0 (.rar); numeric's first volume (.001=1) carries
	// its own header, with continuations at 2.
	merged := make([]parser.ParsedFile, 0, len(files))
	if len(starters) == 1 {
		// A headerless first volume only exists for the roll (.rar + .r00..) and
		// numeric (.001 + .002..) schemes; the part scheme carries its header in
		// part01, so a lone unrecognized file there is foreign — leave it alone.
		var firstName string
		var contStart int
		switch scheme {
		case schemeRoll:
			firstName, contStart = obfuscatedUnifiedBase+".rar", 1
		case schemeNumeric:
			firstName, contStart = obfuscatedUnifiedBase+".001", 2
		default:
			return groups
		}
		if recognized[0].num != contStart {
			return groups // continuations don't slot in right after the first volume
		}
		st := starters[0]
		st.Filename = firstName
		merged = append(merged, st)
	} else {
		// No separate starter: the lowest recognized ordinal carries the header. A roll
		// set may begin at .rar (0) or, when there is no plain .rar, at .r00 (1);
		// part/numeric always begin at ordinal 1.
		validStart := recognized[0].num == 1 || (scheme == schemeRoll && recognized[0].num == 0)
		if !validStart {
			return groups
		}
	}

	for _, v := range recognized {
		vf := v.file
		vf.Filename = canonicalVolumeName(obfuscatedUnifiedBase, scheme, v.num)
		merged = append(merged, vf)
	}

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
