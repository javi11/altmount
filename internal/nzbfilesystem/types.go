package nzbfilesystem

import (
	"path/filepath"
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

	// Replace backslashes with forward slashes first
	path = strings.ReplaceAll(path, "\\", "/")

	// Clean the path using filepath.Clean
	cleaned := filepath.Clean(path)

	// Remove trailing slashes and backslashes
	cleaned = strings.TrimRight(cleaned, "/\\")

	// Ensure we don't return empty string after trimming (e.g. if path was just slashes)
	if cleaned == "" || cleaned == "." {
		return RootPath
	}

	return cleaned
}
