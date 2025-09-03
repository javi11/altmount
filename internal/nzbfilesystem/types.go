package nzbfilesystem

import (
	"strings"
)

// normalizePath normalizes file paths for consistent database lookups
// Removes trailing slashes except for root path "/"
func normalizePath(path string) string {
	// Handle empty path
	if path == "" {
		return RootPath
	}

	// Handle root path - keep as is
	if path == RootPath {
		return path
	}

	// Remove trailing slashes for all other paths
	return strings.TrimRight(path, "/")
}
