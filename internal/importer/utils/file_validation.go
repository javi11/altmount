package utils

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/javi11/altmount/internal/importer/parser"
)

// sampleProofPattern matches filenames containing "sample" or "proof" at word boundaries
// Pattern: (^|[\W_])(sample|proof) - matches at start or after non-word/underscore
var sampleProofPattern = regexp.MustCompile(`(?i)(^|[\W_])(sample|proof)`)

// createExtensionMap converts a slice of extensions to a map for O(1) lookups
func createExtensionMap(extensions []string) map[string]bool {
	extMap := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		// Normalize to lowercase for case-insensitive comparison
		extMap[strings.ToLower(ext)] = true
	}
	return extMap
}

// IsAllowedFile checks if a filename has an allowed extension
// If allowedExtensions is empty, all files are allowed
func IsAllowedFile(filename string, allowedExtensions []string) bool {
	if filename == "" {
		return false
	}

	// Reject files with sample/proof in their name
	if sampleProofPattern.MatchString(filename) {
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
// If allowedExtensions is empty, all file types are allowed but sample/proof files are still rejected
func HasAllowedFilesInRegular(regularFiles []parser.ParsedFile, allowedExtensions []string) bool {
	for _, file := range regularFiles {
		if IsAllowedFile(file.Filename, allowedExtensions) {
			return true
		}
	}
	return false
}
