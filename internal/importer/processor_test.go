package importer

import (
	"testing"

	"github.com/javi11/altmount/internal/importer/filesystem"
	"github.com/javi11/altmount/internal/importer/parser"
)

func TestNormalizeReleaseFilename(t *testing.T) {
	tests := []struct {
		name             string
		nzbFilename      string
		originalFilename string
		expected         string
	}{
		{
			name:             "keeps single extension",
			nzbFilename:      "file.mkv.nzb",
			originalFilename: "file.mkv",
			expected:         "file.mkv",
		},
		{
			name:             "adds missing extension from original",
			nzbFilename:      "obfuscated.nzb",
			originalFilename: "video.mkv",
			expected:         "obfuscated.mkv",
		},
		{
			name:             "avoids duplicate when nzb already has ext",
			nzbFilename:      "[TEST].mp4.nzb",
			originalFilename: "random.mp4",
			expected:         "[TEST].mp4",
		},
		{
			name:             "preserves nzb basename but uses original ext",
			nzbFilename:      "movie.1080p.NZB",
			originalFilename: "source.mkv",
			expected:         "movie.1080p.mkv",
		},
		{
			name:             "no extension in original",
			nzbFilename:      "sample.nzb",
			originalFilename: "filename",
			expected:         "sample",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeReleaseFilename(tt.nzbFilename, tt.originalFilename)
			if got != tt.expected {
				t.Fatalf("normalizeReleaseFilename(%q, %q) = %q, want %q", tt.nzbFilename, tt.originalFilename, got, tt.expected)
			}
		})
	}
}

func TestNormalizeSingleFileVirtualDir(t *testing.T) {
	tests := []struct {
		name        string
		virtualDir  string
		releaseName string
		filename    string
		expected    string
	}{
		{
			name:        "keeps season folder",
			virtualDir:  "/Media/Animes/Show/Season 01",
			releaseName: "Show.S01E01.1080p",
			filename:    "Show.S01E01.1080p.mkv",
			expected:    "/Media/Animes/Show/Season 01",
		},
		{
			name:        "flattens when ends with filename",
			virtualDir:  "/Media/Animes/Show/Season 01/Show.S01E01.1080p.mkv",
			releaseName: "Show.S01E01.1080p",
			filename:    "Show.S01E01.1080p.mkv",
			expected:    "/Media/Animes/Show/Season 01",
		},
		{
			name:        "flattens when ends with release name",
			virtualDir:  "/Media/Animes/Show/Season 01/Show.S01E01.1080p",
			releaseName: "Show.S01E01.1080p",
			filename:    "episode.mkv",
			expected:    "/Media/Animes/Show/Season 01",
		},
		{
			name:        "root stays root",
			virtualDir:  "/",
			releaseName: "Anything",
			filename:    "file.mkv",
			expected:    "/",
		},
		{
			name:        "does not flatten when file has path",
			virtualDir:  "/Media/Animes/Show/Season 01",
			releaseName: "Show.S01E01.1080p",
			filename:    "sub/Show.S01E01.1080p.mkv",
			expected:    "/Media/Animes/Show/Season 01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSingleFileVirtualDir(tt.virtualDir, tt.releaseName, tt.filename)
			if got != tt.expected {
				t.Fatalf("normalizeSingleFileVirtualDir(%q, %q, %q) = %q, want %q", tt.virtualDir, tt.releaseName, tt.filename, got, tt.expected)
			}
		})
	}
}

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
			filename:       `movie.mkv\\movie.mkv`,
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
			expectedParent: "/base",
			expectedName:   "movie.mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := parser.ParsedFile{Filename: tt.filename}
			parent, name := filesystem.DetermineFileLocation(file, tt.baseDir)
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
			expected:     "/",
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
			expected:     "/",
		},
		{
			name:         "file with spaces",
			nzbPath:      "/downloads/sonarr/Movie Name (2023).mkv",
			relativePath: "/downloads/sonarr",
			expected:     "/",
		},
		{
			name:         "file in persistent .nzbs directory",
			nzbPath:      "/config/.nzbs/MovieFolder/Movie.nzb",
			relativePath: "/config",
			expected:     "/MovieFolder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filesystem.CalculateVirtualDirectory(tt.nzbPath, tt.relativePath)
			if result != tt.expected {
				t.Errorf("CalculateVirtualDirectory(%q, %q) = %q, want %q", tt.nzbPath, tt.relativePath, result, tt.expected)
			}
		})
	}
}
