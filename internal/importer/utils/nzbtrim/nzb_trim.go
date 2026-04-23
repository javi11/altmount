package nzbtrim

import (
	"path/filepath"
	"strings"
)

// TrimNzbExtension removes .nzb or .nzb.gz from a filename (case-insensitive)
func TrimNzbExtension(filename string) string {
	lower := strings.ToLower(filename)
	if strings.HasSuffix(lower, ".nzb.gz") {
		return filename[:len(filename)-7]
	}
	if strings.HasSuffix(lower, ".nzb") {
		return filename[:len(filename)-4]
	}
	// Fallback to standard extension removal if it's not a known NZB extension
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

// HasNzbExtension returns true when filename ends with .nzb or .nzb.gz (case-insensitive).
func HasNzbExtension(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".nzb") || strings.HasSuffix(lower, ".nzb.gz")
}
