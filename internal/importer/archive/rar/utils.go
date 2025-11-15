package rar

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"

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

// hasExtension checks if a filename has an extension
func hasExtension(filename string) bool {
	ext := filepath.Ext(filename)
	return ext != ""
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
	if allFilesNoExt && !hasExtension(filename) && baseFilename != "" {
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
