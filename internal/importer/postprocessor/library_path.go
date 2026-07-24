package postprocessor

import (
	"path"
	"strings"
)

// buildLibraryRelPath strips the configured CompleteDir and category prefixes
// from relPath (if already present) and reconstructs the canonical
// [CompleteDir]/[Category]/<remainder> layout.
//
// relPath may use either '/' or OS-native separators; the result always uses
// '/'. Backslashes are always converted to forward slashes regardless of host
// OS — filepath.ToSlash is a no-op on non-Windows and would miss the Windows
// shape returned by filepath.Rel/filepath.WalkDir when callers feed those in.
// Stripping is done once, so a path that already includes either prefix is
// not double-prefixed.
func buildLibraryRelPath(relPath, completeDir, category string) string {
	relPath = strings.TrimPrefix(strings.ReplaceAll(relPath, `\`, "/"), "/")
	completeDir = strings.Trim(strings.ReplaceAll(completeDir, `\`, "/"), "/")
	category = strings.Trim(strings.ReplaceAll(category, `\`, "/"), "/")

	if completeDir != "" {
		if after, ok := strings.CutPrefix(relPath, completeDir+"/"); ok {
			relPath = after
		} else if relPath == completeDir {
			relPath = ""
		}
	}
	if category != "" {
		if after, ok := strings.CutPrefix(relPath, category+"/"); ok {
			relPath = after
		} else if relPath == category {
			relPath = ""
		}
	}

	parts := make([]string, 0, 3)
	if completeDir != "" {
		parts = append(parts, completeDir)
	}
	if category != "" {
		parts = append(parts, category)
	}
	parts = append(parts, relPath)

	// category/relPath are client-reachable (SABnzbd-emulation/Stremio/
	// manual-upload) and neither is validated before reaching here.
	// path.Clean/path.Join only clamp ".." for an absolute (leading "/")
	// path - a relative result would let a smuggled "../../.." survive
	// straight through to the real filesystem join in the symlink/STRM
	// writers downstream. Rooting the join before cleaning, then trimming
	// the root back off, forces that clamp regardless of how deep the
	// traversal attempt goes.
	joined := path.Join(parts...)
	cleaned := path.Clean("/" + joined)
	return strings.TrimPrefix(cleaned, "/")
}
