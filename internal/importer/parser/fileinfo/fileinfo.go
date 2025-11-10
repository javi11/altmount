package fileinfo

import (
	"crypto/md5"
	"regexp"
	"strings"

	"github.com/javi11/altmount/internal/importer/parser/par2"
)

// GetFileInfos extracts file information from NZB files with first segment data
// Similar to C# GetFileInfosStep.GetFileInfos
func GetFileInfos(
	files []*NzbFileWithFirstSegment,
	par2Descriptors map[[16]byte]*par2.FileDescriptor,
) []*FileInfo {
	fileInfos := make([]*FileInfo, 0, len(files))
	for _, file := range files {
		info := getFileInfo(file, par2Descriptors)
		fileInfos = append(fileInfos, info)
	}

	return fileInfos
}

// getFileInfo extracts information for a single file
func getFileInfo(
	file *NzbFileWithFirstSegment,
	hashToDescMap map[[16]byte]*par2.FileDescriptor,
) *FileInfo {
	par2Filename := ""
	par2FileSize := int64(0)

	if len(hashToDescMap) > 0 {
		// Calculate MD5 hash of first 16KB for PAR2 matching
		md5Hash := md5.Sum(file.First16KB)

		desc, ok := hashToDescMap[md5Hash]
		if ok {
			par2Filename = desc.Name
			par2FileSize = int64(desc.Length)
		}
	}

	subjectFilename := file.NzbFile.Filename

	headerFilename := ""
	if file.Headers != nil {
		headerFilename = file.Headers.FileName
	}

	// Select best filename using priority system
	filename := selectBestFilename(par2Filename, subjectFilename, headerFilename)

	// Determine file size (PAR2 has highest priority)
	var fileSize *int64
	if par2FileSize > 0 {
		fileSize = &par2FileSize
	} else if file.Headers != nil && file.Headers.FileSize > 0 {
		size := int64(file.Headers.FileSize)
		fileSize = &size
	}

	// Detect RAR archives (by magic bytes or extension)
	isRar := HasRarMagic(file.First16KB) || IsRarFile(filename)

	// Detect 7z archives (by extension only, no magic bytes check for 7z)
	is7z := Is7zFile(filename)

	isPar2Archive := IsPar2File(filename)

	return &FileInfo{
		NzbFile:       *file.NzbFile,
		Filename:      filename,
		ReleaseDate:   file.ReleaseDate,
		IsPar2Archive: isPar2Archive,
		FileSize:      fileSize,
		IsRar:         isRar,
		Is7z:          is7z,
		YencHeaders:   file.Headers,
		First16KB:     file.First16KB,
		OriginalIndex: file.OriginalIndex,
	}
}

// selectBestFilename selects the best filename using priority system
// Priority: PAR2 (3) > Subject (2) > Header (1)
// With adjustments for obfuscation, important types, and extension length
func selectBestFilename(par2Filename, subjectFilename, headerFilename string) string {
	type candidate struct {
		filename string
		priority int
	}

	candidates := []candidate{
		{filename: par2Filename, priority: getFilenamePriority(par2Filename, 3)},
		{filename: subjectFilename, priority: getFilenamePriority(subjectFilename, 2)},
		{filename: headerFilename, priority: getFilenamePriority(headerFilename, 1)},
	}

	// Find candidate with highest priority
	bestCandidate := candidate{filename: "", priority: -5000}
	for _, c := range candidates {
		if c.filename != "" && c.priority > bestCandidate.priority {
			bestCandidate = c
		}
	}

	return bestCandidate.filename
}

// getFilenamePriority calculates priority score for a filename
// Higher score = better filename
func getFilenamePriority(filename string, startingPriority int) int {
	priority := startingPriority

	// Empty filename gets very low priority
	if strings.TrimSpace(filename) == "" {
		return priority - 5000
	}

	// Obfuscated filenames get -1000 penalty
	if isProbablyObfuscated(filename) {
		priority -= 1000
	}

	// Important file types get +50 bonus
	if IsImportantFileType(filename) {
		priority += 50
	}

	// Valid extension length (2-4 chars) gets +10 bonus
	if HasValidExtensionLength(filename) {
		priority += 10
	}

	return priority
}

// isProbablyObfuscated checks if a filename is likely obfuscated
// Based on SABnzbd's deobfuscation algorithm:
// https://github.com/sabnzbd/sabnzbd/blob/64034c5636563b66360aa9dfc1a0b624f4db5cc3/sabnzbd/deobfuscate_filenames.py#L105
func isProbablyObfuscated(filename string) bool {
	if filename == "" {
		return false
	}

	// Extract base filename without extension
	// Find last dot position
	lastDot := strings.LastIndex(filename, ".")
	baseFilename := filename
	if lastDot > 0 {
		baseFilename = filename[:lastDot]
	}

	// ---
	// First: patterns that are certainly obfuscated

	// 32 hex digits (MD5-like hash)
	// Example: b082fa0beaa644d3aa01045d5b8d0b36.mkv
	if matched, _ := regexp.MatchString(`^[a-f0-9]{32}$`, baseFilename); matched {
		return true
	}

	// 40+ lowercase hex digits and/or dots
	// Example: 0675e29e9abfd2.f7d069dab0b853283cc1b069a25f82.6547
	if matched, _ := regexp.MatchString(`^[a-f0-9.]{40,}$`, baseFilename); matched {
		return true
	}

	// 30+ hex digits with 2+ sets of square brackets
	// Example: [BlaBla] something 5937bc5e32146e.bef89a622e4a23f07b0d3757ad5e8a.a02b264e [More]
	if matched, _ := regexp.MatchString(`[a-f0-9]{30}`, baseFilename); matched {
		bracketCount := strings.Count(baseFilename, "[")
		if bracketCount >= 2 {
			return true
		}
	}

	// Starts with 'abc.xyz' (common obfuscation pattern)
	// Example: abc.xyz.a4c567edbcbf27.BLA
	if matched, _ := regexp.MatchString(`^abc\.xyz`, baseFilename); matched {
		return true
	}

	// ---
	// Then: patterns that are NOT obfuscated (typical, clear names)

	// Count character types
	decimals := 0
	upperChars := 0
	lowerChars := 0
	spacesDots := 0

	for _, char := range baseFilename {
		switch {
		case char >= '0' && char <= '9':
			decimals++
		case char >= 'A' && char <= 'Z':
			upperChars++
		case char >= 'a' && char <= 'z':
			lowerChars++
		case char == ' ' || char == '.' || char == '_':
			spacesDots++
		}
	}

	// Example: "Great Distro"
	// Has uppercase, lowercase, and space-like separators
	if upperChars >= 2 && lowerChars >= 2 && spacesDots >= 1 {
		return false
	}

	// Example: "this is a download"
	// Multiple spaces/dots/underscores indicate readable name
	if spacesDots >= 3 {
		return false
	}

	// Example: "Beast 2020"
	// Has letters, digits, and separators
	if (upperChars+lowerChars >= 4) && decimals >= 4 && spacesDots >= 1 {
		return false
	}

	// Example: "Catullus"
	// Starts with capital, mostly lowercase (typical proper noun/title)
	if len(baseFilename) > 0 {
		firstChar := rune(baseFilename[0])
		if firstChar >= 'A' && firstChar <= 'Z' && lowerChars > 2 {
			if upperChars == 0 || float64(upperChars)/float64(lowerChars) <= 0.25 {
				return false
			}
		}
	}

	// Finally: default to obfuscated
	return true
}

// IsPar2File checks if a filename is a PAR2 archive
func IsPar2File(filename string) bool {
	return strings.HasSuffix(strings.ToLower(filename), ".par2")
}
