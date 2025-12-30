package bluray

import (
	"path/filepath"
	"strings"
)

// BDMVStructure represents a detected Blu-ray disc structure
type BDMVStructure struct {
	BDMVPath    string   // Root path to BDMV folder (e.g., "BDMV/")
	IndexFile   string   // Path to index.bdmv
	StreamFiles []string // List of .m2ts files in STREAM/
	ClipFiles   []string // List of .clpi files in CLIPINF/
	PlaylistFiles []string // List of .mpls files in PLAYLIST/
}

// IsBlurayStructure detects if a list of file paths represents a Blu-ray disc structure
// It looks for the characteristic BDMV folder with index.bdmv and STREAM directory
func IsBlurayStructure(filePaths []string) bool {
	hasBDMV := false
	hasIndex := false
	hasStream := false

	for _, path := range filePaths {
		// Normalize path separators
		normalizedPath := filepath.ToSlash(strings.ToUpper(path))

		// Check for BDMV directory
		if strings.Contains(normalizedPath, "BDMV/") {
			hasBDMV = true
		}

		// Check for index.bdmv file
		if strings.Contains(normalizedPath, "BDMV/INDEX.BDMV") {
			hasIndex = true
		}

		// Check for STREAM directory with .m2ts files
		if strings.Contains(normalizedPath, "BDMV/STREAM/") && strings.HasSuffix(normalizedPath, ".M2TS") {
			hasStream = true
		}
	}

	return hasBDMV && hasIndex && hasStream
}

// AnalyzeBDMVStructure analyzes a list of file paths and extracts the BDMV structure
func AnalyzeBDMVStructure(filePaths []string) *BDMVStructure {
	if !IsBlurayStructure(filePaths) {
		return nil
	}

	structure := &BDMVStructure{
		StreamFiles:   make([]string, 0),
		ClipFiles:     make([]string, 0),
		PlaylistFiles: make([]string, 0),
	}

	for _, path := range filePaths {
		normalizedPath := filepath.ToSlash(path)
		upperPath := strings.ToUpper(normalizedPath)

		// Find BDMV root path
		if structure.BDMVPath == "" && strings.Contains(upperPath, "BDMV/") {
			idx := strings.Index(upperPath, "BDMV/")
			structure.BDMVPath = normalizedPath[:idx+5] // Include "BDMV/"
		}

		// Collect index.bdmv
		if strings.Contains(upperPath, "BDMV/INDEX.BDMV") {
			structure.IndexFile = normalizedPath
		}

		// Collect STREAM .m2ts files
		if strings.Contains(upperPath, "BDMV/STREAM/") && strings.HasSuffix(upperPath, ".M2TS") {
			structure.StreamFiles = append(structure.StreamFiles, normalizedPath)
		}

		// Collect CLIPINF .clpi files
		if strings.Contains(upperPath, "BDMV/CLIPINF/") && strings.HasSuffix(upperPath, ".CLPI") {
			structure.ClipFiles = append(structure.ClipFiles, normalizedPath)
		}

		// Collect PLAYLIST .mpls files
		if strings.Contains(upperPath, "BDMV/PLAYLIST/") && strings.HasSuffix(upperPath, ".MPLS") {
			structure.PlaylistFiles = append(structure.PlaylistFiles, normalizedPath)
		}
	}

	return structure
}

// GetStreamBaseName extracts the base name of a stream file (e.g., "00000" from "00000.m2ts")
func GetStreamBaseName(streamPath string) string {
	base := filepath.Base(streamPath)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

// GetCorrespondingClipInfo finds the corresponding .clpi file for a given .m2ts stream
func GetCorrespondingClipInfo(streamPath string, clipFiles []string) string {
	baseName := GetStreamBaseName(streamPath)
	upperBaseName := strings.ToUpper(baseName)

	for _, clipFile := range clipFiles {
		clipBase := GetStreamBaseName(clipFile)
		if strings.ToUpper(clipBase) == upperBaseName {
			return clipFile
		}
	}

	return ""
}
