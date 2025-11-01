package utils

import (
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/importer/archive/rar"
	"github.com/javi11/altmount/internal/importer/archive/sevenzip"
	"github.com/javi11/altmount/internal/importer/parser"
)

// createExtensionMap converts a slice of extensions to a map for O(1) lookups
func createExtensionMap(extensions []string) map[string]bool {
	extMap := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		// Normalize to lowercase for case-insensitive comparison
		extMap[strings.ToLower(ext)] = true
	}
	return extMap
}

// isAllowedFile checks if a filename has an allowed extension
// If allowedExtensions is empty, all files are allowed
func isAllowedFile(filename string, allowedExtensions []string) bool {
	if filename == "" {
		return false
	}

	// Empty list = allow all files
	if len(allowedExtensions) == 0 {
		return true
	}

	ext := strings.ToLower(filepath.Ext(filename))
	extMap := createExtensionMap(allowedExtensions)
	return extMap[ext]
}

// HasAllowedFilesInRegular checks if any regular (non-archive) files match allowed extensions
// If allowedExtensions is empty, returns true (all files allowed)
func HasAllowedFilesInRegular(regularFiles []parser.ParsedFile, allowedExtensions []string) bool {
	// Empty list = allow all files
	if len(allowedExtensions) == 0 {
		return true
	}

	for _, file := range regularFiles {
		if isAllowedFile(file.Filename, allowedExtensions) {
			return true
		}
	}
	return false
}

// HasAllowedFilesInRarArchive checks if any files within RAR archive contents match allowed extensions
// If allowedExtensions is empty, returns true (all files allowed)
func HasAllowedFilesInRarArchive(rarContents []rar.Content, allowedExtensions []string) bool {
	// Empty list = allow all files
	if len(allowedExtensions) == 0 {
		return true
	}

	for _, content := range rarContents {
		// Skip directories
		if content.IsDirectory {
			continue
		}
		// Check both the internal path and filename
		if isAllowedFile(content.InternalPath, allowedExtensions) || isAllowedFile(content.Filename, allowedExtensions) {
			return true
		}
	}
	return false
}

// HasAllowedFilesIn7zipArchive checks if any files within 7zip archive contents match allowed extensions
// If allowedExtensions is empty, returns true (all files allowed)
func HasAllowedFilesIn7zipArchive(sevenZipContents []sevenzip.Content, allowedExtensions []string) bool {
	// Empty list = allow all files
	if len(allowedExtensions) == 0 {
		return true
	}

	for _, content := range sevenZipContents {
		// Skip directories
		if content.IsDirectory {
			continue
		}
		// Check both the internal path and filename
		if isAllowedFile(content.InternalPath, allowedExtensions) || isAllowedFile(content.Filename, allowedExtensions) {
			return true
		}
	}
	return false
}

// HasAllowedFiles checks if there are any files matching allowed extensions in regular files or archive contents
// If allowedExtensions is empty, returns true (all files allowed)
// This is a convenience function that combines all the individual checks
func HasAllowedFiles(regularFiles []parser.ParsedFile, rarContents []rar.Content, sevenZipContents []sevenzip.Content, allowedExtensions []string) bool {
	// Empty list = allow all files
	if len(allowedExtensions) == 0 {
		return true
	}

	// Check regular files
	if HasAllowedFilesInRegular(regularFiles, allowedExtensions) {
		return true
	}

	// Check RAR archive contents
	if HasAllowedFilesInRarArchive(rarContents, allowedExtensions) {
		return true
	}

	// Check 7zip archive contents
	if HasAllowedFilesIn7zipArchive(sevenZipContents, allowedExtensions) {
		return true
	}

	return false
}
