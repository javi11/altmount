package utils

import (
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/importer/archive/rar"
	"github.com/javi11/altmount/internal/importer/archive/sevenzip"
	"github.com/javi11/altmount/internal/importer/parser"
)

// Common video file extensions
var videoExtensions = map[string]bool{
	".3g2":   true,
	".3gp":   true,
	".avi":   true,
	".flv":   true,
	".h264":  true,
	".m4v":   true,
	".mkv":   true,
	".mov":   true,
	".mp4":   true,
	".mpeg":  true,
	".mpg":   true,
	".ogv":   true,
	".ts":    true,
	".vob":   true,
	".webm":  true,
	".wmv":   true,
	".m2ts":  true,
}

// isVideoFile checks if a filename has a video extension
func isVideoFile(filename string) bool {
	if filename == "" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(filename))
	return videoExtensions[ext]
}

// HasVideoFilesInRegular checks if any regular (non-archive) files are video files
func HasVideoFilesInRegular(regularFiles []parser.ParsedFile) bool {
	for _, file := range regularFiles {
		if isVideoFile(file.Filename) {
			return true
		}
	}
	return false
}

// HasVideoFilesInRarArchive checks if any files within RAR archive contents are video files
func HasVideoFilesInRarArchive(rarContents []rar.Content) bool {
	for _, content := range rarContents {
		// Skip directories
		if content.IsDirectory {
			continue
		}
		// Check both the internal path and filename
		if isVideoFile(content.InternalPath) || isVideoFile(content.Filename) {
			return true
		}
	}
	return false
}

// HasVideoFilesIn7zipArchive checks if any files within 7zip archive contents are video files
func HasVideoFilesIn7zipArchive(sevenZipContents []sevenzip.Content) bool {
	for _, content := range sevenZipContents {
		// Skip directories
		if content.IsDirectory {
			continue
		}
		// Check both the internal path and filename
		if isVideoFile(content.InternalPath) || isVideoFile(content.Filename) {
			return true
		}
	}
	return false
}

// HasVideoFiles checks if there are any video files in regular files or archive contents
// This is a convenience function that combines all the individual checks
func HasVideoFiles(regularFiles []parser.ParsedFile, rarContents []rar.Content, sevenZipContents []sevenzip.Content) bool {
	// Check regular files
	if HasVideoFilesInRegular(regularFiles) {
		return true
	}

	// Check RAR archive contents
	if HasVideoFilesInRarArchive(rarContents) {
		return true
	}

	// Check 7zip archive contents
	if HasVideoFilesIn7zipArchive(sevenZipContents) {
		return true
	}

	return false
}
