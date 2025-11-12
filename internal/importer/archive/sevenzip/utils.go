package sevenzip

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/javi11/altmount/internal/importer/archive"
)

var (
	// Pattern for 7zip multi-part: filename.7z.001, filename.7z.002
	sevenZipPartPatternNumber = regexp.MustCompile(`\.7z\.(\d+)$`)
	// Pattern for numeric extensions: filename.001, filename.002
	numericPatternNumber = regexp.MustCompile(`\.(\d+)$`)
)

// hasExtension checks if a filename has an extension
func hasExtension(filename string) bool {
	ext := filepath.Ext(filename)
	return ext != ""
}

// normalize7zPartFilename normalizes 7zip part filenames while preserving original number formatting
// If allFilesNoExt is true, uses baseFilename for all parts with .XXX extension
// where XXX is the 0-based part number (index) with zero-padding based on totalFiles
// Examples:
//   - "movie.7z.001" -> "movie.7z.001" (preserves leading zeros)
//   - "archive.01" -> "archive.01" (preserves 2-digit format)
//   - "movie.7z" -> "movie.7z" (no change for non-part files)
//   - Files ["abc", "def", "xyz"] with allFilesNoExt=true, baseFilename="abc", totalFiles=3:
//   - index=0 -> "abc.001"
//   - index=1 -> "abc.002"
//   - index=2 -> "abc.003"
func normalize7zPartFilename(filename string, index int, allFilesNoExt bool, totalFiles int, baseFilename string) string {
	// If all files have no extension, use baseFilename with .XXX extension
	// This ensures all parts of the same archive have the same base filename
	// Using 7zip multi-volume convention: .001, .002, .003, etc. (1-based)
	if allFilesNoExt && !hasExtension(filename) && baseFilename != "" {
		// Calculate padding width based on total number of files (1-based, so totalFiles)
		width := len(strconv.Itoa(totalFiles))
		// Format with zero-padding (convert 0-based index to 1-based: index+1)
		paddedPartNum := fmt.Sprintf("%0*d", width, index+1)
		return baseFilename + "." + paddedPartNum
	}

	// Pattern 1: filename.7z.###
	if matches := sevenZipPartPatternNumber.FindStringSubmatch(filename); len(matches) > 1 {
		partNumStr := matches[1]
		// Validate it's a valid number (used for sorting logic elsewhere)
		if num := archive.ParseInt(partNumStr); num >= 0 {
			// Keep original format with leading zeros preserved
			return sevenZipPartPatternNumber.ReplaceAllString(filename, ".7z."+partNumStr)
		}
	}

	// Pattern 2: filename.###
	if matches := numericPatternNumber.FindStringSubmatch(filename); len(matches) > 1 {
		partNumStr := matches[1]
		// Validate it's a valid number (used for sorting logic elsewhere)
		if num := archive.ParseInt(partNumStr); num >= 0 {
			// Keep original format with leading zeros preserved
			return numericPatternNumber.ReplaceAllString(filename, "."+partNumStr)
		}
	}

	// No pattern matched, return original filename
	return filename
}
