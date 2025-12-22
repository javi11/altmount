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
	// even if it has "sample" or "proof" in the name.
	if size > 200*1024*1024 {
		return false
	}

	lower := strings.ToLower(filename)
	
	// Standalone "sample" or "proof" check with common separators
	for _, word := range []string{"sample", "proof"} {
		// Use a simple check for common patterns: 
		// .word., -word-, _word_, word., .word, etc.
		// We want to avoid matching words like "Proof" inside a larger title if it's not a standalone component.
		
		// Pattern: Word must be at the start or preceded by a separator
		// AND must be at the end or followed by a separator
		
		startIdx := strings.Index(lower, word)
		for startIdx != -1 {
			endIdx := startIdx + len(word)
			
			// Check preceding character
			validStart := startIdx == 0
			if !validStart {
				prevChar := lower[startIdx-1]
				if prevChar == '.' || prevChar == '_' || prevChar == '-' || prevChar == ' ' {
					validStart = true
				}
			}
			
			// Check following character
			validEnd := endIdx == len(lower)
			if !validEnd {
				nextChar := lower[endIdx]
				// Include dot, underscore, dash, space AND the extension dot
				if nextChar == '.' || nextChar == '_' || nextChar == '-' || nextChar == ' ' {
					validEnd = true
				}
			}
			
			if validStart && validEnd {
				// Special case: If it's a long filename and the word is "proof" or "sample",
				// it might still be a title. However, usually samples are short or have 
				// "sample" very near the end.
				
				// For "Proof" specifically, it's very common in titles like "Spell of Proof".
				// We'll allow it if it's preceded by "of ", "the ", etc.
				if word == "proof" && startIdx >= 3 {
					prefix := lower[max(0, startIdx-4) : startIdx]
					if strings.Contains(prefix, "of ") || strings.Contains(prefix, ".of.") || 
					   strings.Contains(prefix, "the ") || strings.Contains(prefix, ".the.") {
						// Likely part of a title, skip this match and continue searching
						startIdx = strings.Index(lower[endIdx:], word)
						if startIdx != -1 {
							startIdx += endIdx
						}
						continue
					}
				}

				return true
			}
			
			// Move to next occurrence
			nextMatch := strings.Index(lower[endIdx:], word)
			if nextMatch == -1 {
				break
			}
			startIdx = endIdx + nextMatch
		}
	}
	
	return false
}

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

// IsAllowedFile checks if a filename has an allowed extension
// If allowedExtensions is empty, all files are allowed
// size is used to prevent false positives for sample/proof checks on large files
func IsAllowedFile(filename string, size int64, allowedExtensions []string) bool {
	if filename == "" {
		return false
	}

	// Reject files with sample/proof in their name
	if isSampleOrProof(filename, size) {
		return false
	}

	// Empty list = allow all files
	if len(allowedExtensions) == 0 {
		return true
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	extMap := createExtensionMap(allowedExtensions)
	return extMap[ext]
}

// HasAllowedFilesInRegular checks if any regular (non-archive) files match allowed extensions
// If allowedExtensions is empty, all file types are allowed but sample/proof files are still rejected
func HasAllowedFilesInRegular(regularFiles []parser.ParsedFile, allowedExtensions []string) bool {
	for _, file := range regularFiles {
		if IsAllowedFile(file.Filename, file.Size, allowedExtensions) {
			return true
		}
	}
	return false
}
