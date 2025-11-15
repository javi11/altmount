package archive

import (
	"path/filepath"
	"regexp"
	"strings"
)

// MaxNestingDepth defines the maximum depth for nested archive processing
const MaxNestingDepth = 1

var (
	// RAR part file patterns
	rarPartPattern     = regexp.MustCompile(`\.part\d+\.rar$`)
	rarRPattern        = regexp.MustCompile(`\.r\d+$`)
	rarNumericPattern  = regexp.MustCompile(`\.\d+$`)
	rarMainPattern     = regexp.MustCompile(`\.rar$`)
)

// IsRarPartFile checks if a filename is a RAR part file
func IsRarPartFile(filename string) bool {
	lower := strings.ToLower(filename)

	// Check for common RAR patterns
	if strings.HasSuffix(lower, ".rar") {
		return true
	}
	if rarPartPattern.MatchString(lower) {
		return true
	}
	if rarRPattern.MatchString(lower) {
		return true
	}

	// Check for numeric extensions (e.g., .001, .002)
	// Must have at least 3 digits to avoid false positives
	if rarNumericPattern.MatchString(lower) {
		ext := filepath.Ext(lower)
		if len(ext) >= 4 { // "." + at least 3 digits
			return true
		}
	}

	return false
}

// GetRarBaseName extracts the base name from a RAR part filename
// Examples:
//   movie.part001.rar -> movie
//   movie.r00 -> movie
//   movie.001 -> movie
//   movie.rar -> movie
func GetRarBaseName(filename string) string {
	lower := strings.ToLower(filename)

	// Remove .part###.rar
	if rarPartPattern.MatchString(lower) {
		// Find .part and remove everything after it
		if idx := strings.Index(lower, ".part"); idx != -1 {
			return filename[:idx]
		}
	}

	// Remove .r## or .r###
	if rarRPattern.MatchString(lower) {
		if idx := strings.LastIndex(lower, ".r"); idx != -1 {
			return filename[:idx]
		}
	}

	// Remove .rar
	if strings.HasSuffix(lower, ".rar") {
		return filename[:len(filename)-4]
	}

	// Remove numeric extension (.###)
	if rarNumericPattern.MatchString(lower) {
		ext := filepath.Ext(lower)
		if len(ext) >= 4 {
			return filename[:len(filename)-len(ext)]
		}
	}

	return filename
}

// RarPartGroup represents a group of RAR part files with the same base name
type RarPartGroup struct {
	BaseName string
	Files    []string
}

// GroupRarParts groups filenames by their RAR base name
func GroupRarParts(filenames []string) []RarPartGroup {
	groups := make(map[string][]string)

	for _, filename := range filenames {
		if IsRarPartFile(filename) {
			baseName := GetRarBaseName(filename)
			groups[baseName] = append(groups[baseName], filename)
		}
	}

	// Convert map to slice
	result := make([]RarPartGroup, 0, len(groups))
	for baseName, files := range groups {
		result = append(result, RarPartGroup{
			BaseName: baseName,
			Files:    files,
		})
	}

	return result
}

// ShouldProcessNested checks if nested archive processing should continue
// based on the current depth
func ShouldProcessNested(depth int) bool {
	return depth < MaxNestingDepth
}
