package utils

import "strings"

// SanitizePathSegment neutralizes directory-traversal in an untrusted string
// (a queue item's Category, or a filename/subpath pulled out of NZB content)
// before it's used to build a real filesystem path. Both Category and NZB
// filenames are attacker-influenceable: Category comes straight from
// SABnzbd-emulation/Stremio/manual-upload API calls, and filenames come from
// an NZB poster's own file entries, which any indexer an *arr app auto-grabs
// from can serve with no human review.
//
// Backslashes are normalized to forward slashes first (Windows-style paths
// can appear in yEnc headers and API input alike), then each '/'-separated
// segment is checked - if any segment is ".." or ".", the whole string is
// considered unsafe and an empty string is returned rather than trying to
// salvage a partial path, so callers fall back to a safe default (an empty
// category folder, a flat/synthesized filename) instead of silently walking
// outside the intended directory.
func SanitizePathSegment(s string) string {
	if s == "" {
		return ""
	}
	normalized := strings.ReplaceAll(s, `\`, "/")
	normalized = strings.Trim(normalized, "/")
	if normalized == "" {
		return ""
	}
	for part := range strings.SplitSeq(normalized, "/") {
		if part == ".." || part == "." || part == "" {
			return ""
		}
	}
	return normalized
}
