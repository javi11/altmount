package filesystem

import (
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
)

func TestDetermineFileLocation(t *testing.T) {
	tests := []struct {
		name           string
		filename       string
		baseDir        string
		expectedParent string
		expectedName   string
	}{
		{
			name:           "simple file",
			filename:       "movie.mkv",
			baseDir:        "/base",
			expectedParent: "/base",
			expectedName:   "movie.mkv",
		},
		{
			name:           "nested file",
			filename:       "folder/movie.mkv",
			baseDir:        "/base",
			expectedParent: "/base/folder",
			expectedName:   "movie.mkv",
		},
		{
			name:           "redundant folder (exact match)",
			filename:       "movie.mkv/movie.mkv",
			baseDir:        "/base",
			expectedParent: "/base",
			expectedName:   "movie.mkv",
		},
		{
			name:           "redundant folder with leading slash",
			filename:       "/movie.mkv/movie.mkv",
			baseDir:        "/base",
			expectedParent: "/base",
			expectedName:   "movie.mkv",
		},
		{
			name:           "redundant folder with backslashes",
			filename:       `movie.mkv\movie.mkv`,
			baseDir:        "/base",
			expectedParent: "/base",
			expectedName:   "movie.mkv",
		},
		{
			name:           "nested redundant folder",
			filename:       "series/season1/episode1.mkv/episode1.mkv",
			baseDir:        "/base",
			expectedParent: "/base/series/season1",
			expectedName:   "episode1.mkv",
		},
		{
			name:           "non-redundant folder (almost match)",
			filename:       "movie/movie.mkv",
			baseDir:        "/base",
			expectedParent: "/base/movie",
			expectedName:   "movie.mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := parser.ParsedFile{Filename: tt.filename}
			parent, name := DetermineFileLocation(file, tt.baseDir)
			if parent != tt.expectedParent {
				t.Errorf("DetermineFileLocation parent = %q, want %q", parent, tt.expectedParent)
			}
			if name != tt.expectedName {
				t.Errorf("DetermineFileLocation name = %q, want %q", name, tt.expectedName)
			}
		})
	}
}

func TestCalculateVirtualDirectory(t *testing.T) {
	tests := []struct {
		name         string
		nzbPath      string
		relativePath string
		expected     string
	}{
		{
			name:         "file in root of relative path",
			nzbPath:      "/downloads/sonarr/Movie.mkv",
			relativePath: "/downloads/sonarr",
			expected:     "/Movie", // Should create folder based on filename
		},
		{
			name:         "file in subfolder",
			nzbPath:      "/downloads/sonarr/MovieFolder/Movie.mkv",
			relativePath: "/downloads/sonarr",
			expected:     "/MovieFolder",
		},
		{
			name:         "empty relative path",
			nzbPath:      "/downloads/Movie.mkv",
			relativePath: "",
			expected:     "/", // Default behavior for empty relative path
		},
		{
			name:         "file with spaces",
			nzbPath:      "/downloads/sonarr/Movie Name (2023).mkv",
			relativePath: "/downloads/sonarr",
			expected:     "/Movie Name (2023)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateVirtualDirectory(tt.nzbPath, tt.relativePath)
			if result != tt.expected {
				t.Errorf("CalculateVirtualDirectory(%q, %q) = %q, want %q", tt.nzbPath, tt.relativePath, result, tt.expected)
			}
		})
	}
}
