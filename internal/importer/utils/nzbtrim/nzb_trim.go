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

// TrimSurroundingQuotes trims whitespace and strips matched surrounding single or
// double quotes that some posters add to the NZB subject (which otherwise reaches
// the persisted filename and breaks $-anchored archive volume detection). Only a
// matched surrounding pair is removed, so an embedded apostrophe (It's...) is
// preserved. Idempotent and safe on strings shorter than two characters.
func TrimSurroundingQuotes(s string) string {
	s = strings.TrimSpace(s)
	for len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			s = strings.TrimSpace(s[1 : len(s)-1])
			continue
		}
		break
	}
	return s
}
