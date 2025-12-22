package utils

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/javi11/altmount/internal/importer/parser"
)

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
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

// IsAllowedFile checks if a filename has an allowed extension and is not blocked
// If allowedExtensions is empty, all files are allowed
// blockedPatterns is a list of regex patterns to block files
// blockedExtensions is a list of file extensions to block
// size is used to prevent false positives for sample/proof checks on large files
func IsAllowedFile(filename string, size int64, allowedExtensions []string, blockedPatterns []string, blockedExtensions []string) bool {
	if filename == "" {
		return false
	}

	ext := strings.ToLower(filepath.Ext(filename))

	// Check if extension is explicitly blocked
	if len(blockedExtensions) > 0 {
		blockedExtMap := createExtensionMap(blockedExtensions)
		normalizedExt := strings.TrimPrefix(ext, ".")
		if blockedExtMap[normalizedExt] {
			return false
		}
	}

	// Always allow subtitle files (unless explicitly blocked above)
	// These are typically small files where "sample" or "proof" might appear in the name
	// but don't indicate the file itself is a media sample/proof to be rejected.
	if ext == ".srt" || ext == ".sub" || ext == ".idx" || ext == ".vtt" || ext == ".ass" || ext == ".ssa" ||
		ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".nfo" || ext == ".tbn" {
		// Still check if the extension is in the allowed list if it's provided
		if len(allowedExtensions) > 0 {
			normalizedExt := strings.TrimPrefix(ext, ".")
			extMap := createExtensionMap(allowedExtensions)
			return extMap[normalizedExt]
		}
		return true
	}

	// Check blocked patterns (skip for large files if pattern is sample/proof related)
	for _, pattern := range blockedPatterns {
		// If the pattern is specifically for sample/proof, apply size check
		if strings.Contains(strings.ToLower(pattern), "sample") || strings.Contains(strings.ToLower(pattern), "proof") {
			if size > 200*1024*1024 {
				continue
			}
		}
		
		if matched, _ := regexp.MatchString(pattern, filename); matched {
			// Apply exceptions logic for common titles like "Spell of Proof"
			lower := strings.ToLower(filename)
			if strings.Contains(strings.ToLower(pattern), "proof") {
				if strings.Contains(lower, "of proof") || strings.Contains(lower, ".of.proof") || 
				   strings.Contains(lower, "the proof") || strings.Contains(lower, ".the.proof") {
					continue
				}
			}
			return false
		}
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
// If allowedExtensions is empty, all file types are allowed but blocked files are still rejected
func HasAllowedFilesInRegular(regularFiles []parser.ParsedFile, allowedExtensions []string, blockedPatterns []string, blockedExtensions []string) bool {
	for _, file := range regularFiles {
		if IsAllowedFile(file.Filename, file.Size, allowedExtensions, blockedPatterns, blockedExtensions) {
			return true
		}
	}
	return false
}
