package importer

import (
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// isProbablyObfuscated returns true if the filename (or full path) appears to be
// obfuscated, following heuristics translated from the provided Python logic.
// Default outcome is true (assume obfuscated) unless clear patterns indicate otherwise.
//
// Python reference logic summary:
// 1. Definitely obfuscated patterns:
//   - Exactly 32 lowercase hex chars
//   - 40+ chars consisting only of lowercase hex or dots
//   - Contains a 30+ lowercase hex substring AND at least two bracketed words [Word]
//   - Starts with "abc.xyz"
//
// 2. Clearly NOT obfuscated patterns:
//   - At least 2 uppercase, 2 lowercase, and 1 space/dot/underscore
//   - At least 3 space/dot/underscore characters
//   - Letters (upper+lower) >= 4, digits >= 4, and at least 1 space/dot/underscore
//   - Starts with capital, has >2 lowercase, and upper/lower ratio <= 0.25
//
// 3. Otherwise: obfuscated.
// IsProbablyObfuscated returns true if the provided filename/path appears obfuscated.
// See detailed heuristic description above.
func (p *Processor) IsProbablyObfuscated(input string) bool {
	logger := slog.Default()
	if p != nil && p.log != nil {
		logger = p.log
	}

	// Extract filename then its basename without extension
	filename := filepath.Base(input)
	ext := filepath.Ext(filename)
	filebasename := strings.TrimSuffix(filename, ext)
	if filebasename == "" { // empty name -> treat as obfuscated default
		logger.Debug("obfuscation check: empty basename -> default obfuscated", "input", input)
		return true
	}
	logger.Debug("obfuscation check: analyzing", "basename", filebasename)

	// Compile (or reuse) regexes (precompiled at first call via package-level vars could optimize; kept inline for clarity)
	if matched, _ := regexp.MatchString(`^[a-f0-9]{32}$`, filebasename); matched {
		logger.Debug("obfuscation check: exactly 32 hex digits -> obfuscated", "basename", filebasename)
		return true
	}

	if matched, _ := regexp.MatchString(`^[a-f0-9.]{40,}$`, filebasename); matched {
		logger.Debug("obfuscation check: 40+ hex/dot chars -> obfuscated", "basename", filebasename)
		return true
	}

	// Contains 30+ hex substring AND at least two [Word] occurrences
	has30Hex, _ := regexp.MatchString(`[a-f0-9]{30}`, filebasename)
	bracketWords := regexp.MustCompile(`\[\w+\]`).FindAllString(filebasename, -1)
	if has30Hex && len(bracketWords) >= 2 {
		logger.Debug("obfuscation check: 30+ hex plus 2+ [Word] -> obfuscated", "basename", filebasename)
		return true
	}

	if strings.HasPrefix(filebasename, "abc.xyz") { // ^abc\.xyz
		logger.Debug("obfuscation check: starts with abc.xyz -> obfuscated", "basename", filebasename)
		return true
	}

	// Counts for non-obfuscated heuristics
	var digits, uppers, lowers, spacesDots int
	for _, r := range filebasename {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsUpper(r):
			uppers++
		case unicode.IsLower(r):
			lowers++
		}
		if r == ' ' || r == '.' || r == '_' { // space-like set
			spacesDots++
		}
	}

	// Not obfuscated heuristics
	if uppers >= 2 && lowers >= 2 && spacesDots >= 1 {
		logger.Debug("obfuscation check: pattern (>=2 upper, >=2 lower, >=1 space/dot/underscore) -> NOT obfuscated", "basename", filebasename)
		return false
	}
	if spacesDots >= 3 {
		logger.Debug("obfuscation check: pattern (spaces/dots/underscores >=3) -> NOT obfuscated", "basename", filebasename)
		return false
	}
	if (uppers+lowers) >= 4 && digits >= 4 && spacesDots >= 1 {
		logger.Debug("obfuscation check: pattern (letters>=4, digits>=4, space-like>=1) -> NOT obfuscated", "basename", filebasename)
		return false
	}
	// Starts with capital, mostly lowercase
	firstRune, _ := utf8.DecodeRuneInString(filebasename)
	if unicode.IsUpper(firstRune) && lowers > 2 && (lowers > 0) && float64(uppers)/float64(lowers) <= 0.25 {
		logger.Debug("obfuscation check: pattern (Capital start, mostly lowercase) -> NOT obfuscated", "basename", filebasename)
		return false
	}

	logger.Debug("obfuscation check: default -> obfuscated", "basename", filebasename)
	return true
}

// (No helper functions required)
