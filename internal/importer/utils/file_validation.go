package utils

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/javi11/altmount/internal/importer/parser"
)

// sampleProofPattern matches filenames that are likely just sample or proof files
// It matches "sample" or "proof" as a standalone word.
var sampleProofPattern = regexp.MustCompile(`(?i)\b(sample|proof)\b`)

// isSampleOrProof checks if a filename looks like a sample or proof file
func isSampleOrProof(filename string, size int64) bool {
	// If file is larger than 200MB, it's likely not a sample/proof file
	if size > 200*1024*1024 {
		return false
	}

	return sampleProofPattern.MatchString(filename)
}

// createExtensionMap converts a slice of extensions to a map for O(1) lookups
func createExtensionMap(extensions []string) map[string]bool {
	extMap := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		ext = strings.ToLower(strings.TrimPrefix(ext, "."))
		extMap[ext] = true
	}
	return extMap
}

// whitelistedExtensions are extensions that should bypass sample/proof checks
// These are typically small files where "sample" or "proof" might appear in the name
// but don't indicate the file itself is a media sample/proof to be rejected.
var whitelistedExtensions = map[string]bool{
	// Subtitles
	".srt": true, ".sub": true, ".idx": true, ".vtt": true, ".ass": true, ".ssa": true,
	// Images (covers, fanart, nfo)
	".jpg": true, ".jpeg": true, ".png": true, ".nfo": true, ".tbn": true,
}

// IsAllowedFile checks if a filename has an allowed extension
// If allowedExtensions is empty, all files are allowed
// size is used to prevent false positives for sample/proof checks on large files
func IsAllowedFile(filename string, size int64, allowedExtensions []string) bool {
	if filename == "" {
		return false
	}

	ext := strings.ToLower(filepath.Ext(filename))

	// Always allow subtitle files
	if whitelistedExtensions[ext] {
		// Still check if the extension is in the allowed list if it's provided
		if len(allowedExtensions) > 0 {
			normalizedExt := strings.TrimPrefix(ext, ".")
			extMap := createExtensionMap(allowedExtensions)
			return extMap[normalizedExt]
		}
		return true
	}

	// Check if file is a sample or proof
	if isSampleOrProof(filename, size) {
		return false
	}

	// Empty list = allow all files
	if len(allowedExtensions) == 0 {
		return true
	}

	normalizedExt := strings.TrimPrefix(ext, ".")
	extMap := createExtensionMap(allowedExtensions)
	return extMap[normalizedExt]
}

// HasAllowedFilesInRegular checks if any regular (non-archive) files match allowed extensions
// If allowedExtensions is empty, all file types are allowed
func HasAllowedFilesInRegular(regularFiles []parser.ParsedFile, allowedExtensions []string) bool {
	for _, file := range regularFiles {
		if IsAllowedFile(file.Filename, file.Size, allowedExtensions) {
			return true
		}
	}
	return false
}
