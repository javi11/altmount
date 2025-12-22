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

// whitelistedExtensions are extensions that should bypass sample/proof checks
// These are typically small files where "sample" or "proof" might appear in the name
// but don't indicate the file itself is a media sample/proof to be rejected.
var whitelistedExtensions = map[string]bool{
	// Subtitles
	".srt": true, ".sub": true, ".idx": true, ".vtt": true, ".ass": true, ".ssa": true,
	// Images (covers, fanart, nfo)
	".jpg": true, ".jpeg": true, ".png": true, ".nfo": true, ".tbn": true,
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
	if whitelistedExtensions[ext] {
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
