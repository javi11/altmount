package rar

import (
	"regexp"

	"github.com/javi11/altmount/internal/importer/archive"
)

var (
	// filename.part###.rar (e.g., movie.part001.rar, movie.part01.rar)
	partPattern = regexp.MustCompile(`^(.+)\.part(\d+)\.rar$`)
	// filename.### (numeric extensions like .001, .002)
	numericPattern = regexp.MustCompile(`^(.+)\.(\d+)$`)
	//filename.r## or filename.r### (e.g., movie.r00, movie.r01)
	rPattern             = regexp.MustCompile(`^(.+)\.r(\d+)$`)
	partPatternNumber    = regexp.MustCompile(`\.part(\d+)\.rar$`)
	rPatternNumber       = regexp.MustCompile(`\.r(\d+)$`)
	numericPatternNumber = regexp.MustCompile(`\.(\d+)$`)
)

// normalizeRarPartFilename normalizes RAR part numbers by removing leading zeros
// Examples:
//   - "movie.part010.rar" -> "movie.part10.rar"
//   - "movie.r00" -> "movie.r0"
//   - "archive.001" -> "archive.1"
//   - "movie.rar" -> "movie.rar" (no change for non-part files)
func normalizeRarPartFilename(filename string) string {
	// Pattern 1: filename.part###.rar
	if matches := partPatternNumber.FindStringSubmatch(filename); len(matches) > 1 {
		partNumStr := matches[1]
		if num := archive.ParseInt(partNumStr); num >= 0 {
			// Replace the part number with normalized version (no leading zeros)
			return partPatternNumber.ReplaceAllString(filename, ".part"+archive.FormatInt(num)+".rar")
		}
	}

	// Pattern 2: filename.r##
	if matches := rPatternNumber.FindStringSubmatch(filename); len(matches) > 1 {
		partNumStr := matches[1]
		if num := archive.ParseInt(partNumStr); num >= 0 {
			// Replace the r## part with normalized version
			return rPatternNumber.ReplaceAllString(filename, ".r"+archive.FormatInt(num))
		}
	}

	// Pattern 3: filename.###
	if matches := numericPatternNumber.FindStringSubmatch(filename); len(matches) > 1 {
		partNumStr := matches[1]
		if num := archive.ParseInt(partNumStr); num >= 0 {
			// Replace the numeric extension with normalized version
			return numericPatternNumber.ReplaceAllString(filename, "."+archive.FormatInt(num))
		}
	}

	// No pattern matched, return original filename
	return filename
}
