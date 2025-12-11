package utils

import (
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/stretchr/testify/assert"
)

func TestIsAllowedFile_EmptyExtensions(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{
			name:     "empty extensions allows regular file",
			filename: "movie.mkv",
			expected: true,
		},
		{
			name:     "empty extensions rejects sample file",
			filename: "movie.sample.mkv",
			expected: false,
		},
		{
			name:     "empty extensions rejects proof file",
			filename: "movie.proof.mkv",
			expected: false,
		},
		{
			name:     "empty extensions rejects file with sample in middle",
			filename: "sample.movie.mkv",
			expected: false,
		},
		{
			name:     "empty extensions rejects file with SAMPLE uppercase",
			filename: "movie.SAMPLE.mkv",
			expected: false,
		},
		{
			name:     "empty extensions rejects file with proof at start",
			filename: "proof.movie.mkv",
			expected: false,
		},
		{
			name:     "empty filename is rejected",
			filename: "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAllowedFile(tt.filename, []string{})
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsAllowedFile_WithExtensions(t *testing.T) {
	allowedExts := []string{".mkv", ".mp4"}

	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{
			name:     "allowed extension passes",
			filename: "movie.mkv",
			expected: true,
		},
		{
			name:     "sample file with allowed extension is rejected",
			filename: "movie.sample.mkv",
			expected: false,
		},
		{
			name:     "proof file with allowed extension is rejected",
			filename: "movie.proof.mkv",
			expected: false,
		},
		{
			name:     "disallowed extension fails",
			filename: "movie.avi",
			expected: false,
		},
		{
			name:     "case insensitive extension match",
			filename: "movie.MKV",
			expected: true,
		},
		{
			name:     "mp4 extension passes",
			filename: "video.mp4",
			expected: true,
		},
		{
			name:     "sample with disallowed extension is rejected",
			filename: "movie.sample.avi",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAllowedFile(tt.filename, allowedExts)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasAllowedFilesInRegular_EmptyExtensions(t *testing.T) {
	tests := []struct {
		name     string
		files    []parser.ParsedFile
		expected bool
	}{
		{
			name: "empty extensions allows regular files",
			files: []parser.ParsedFile{
				{Filename: "movie.mkv"},
				{Filename: "video.mp4"},
			},
			expected: true,
		},
		{
			name: "empty extensions rejects only sample files",
			files: []parser.ParsedFile{
				{Filename: "sample.movie.mkv"},
				{Filename: "proof.video.mp4"},
			},
			expected: false,
		},
		{
			name: "empty extensions allows at least one non-sample",
			files: []parser.ParsedFile{
				{Filename: "movie.mkv"},
				{Filename: "sample.movie.mkv"},
			},
			expected: true,
		},
		{
			name:     "empty file list returns false",
			files:    []parser.ParsedFile{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasAllowedFilesInRegular(tt.files, []string{})
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasAllowedFilesInRegular_WithExtensions(t *testing.T) {
	tests := []struct {
		name     string
		files    []parser.ParsedFile
		allowed  []string
		expected bool
	}{
		{
			name: "has matching file",
			files: []parser.ParsedFile{
				{Filename: "movie.mkv"},
				{Filename: "video.mp4"},
			},
			allowed:  []string{".mkv"},
			expected: true,
		},
		{
			name: "no matching files",
			files: []parser.ParsedFile{
				{Filename: "movie.avi"},
				{Filename: "video.wmv"},
			},
			allowed:  []string{".mkv", ".mp4"},
			expected: false,
		},
		{
			name: "sample files are filtered out",
			files: []parser.ParsedFile{
				{Filename: "movie.sample.mkv"},
				{Filename: "video.proof.mkv"},
			},
			allowed:  []string{".mkv"},
			expected: false,
		},
		{
			name: "mixed files with at least one valid",
			files: []parser.ParsedFile{
				{Filename: "movie.mkv"},
				{Filename: "video.avi"},
				{Filename: "sample.mkv"},
			},
			allowed:  []string{".mkv"},
			expected: true,
		},
		{
			name:     "empty file list returns false",
			files:    []parser.ParsedFile{},
			allowed:  []string{".mkv"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasAllowedFilesInRegular(tt.files, tt.allowed)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsAllowedFile_SampleProofPatterns(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{
			name:     "sample at word boundary",
			filename: "movie-sample.mkv",
			expected: false,
		},
		{
			name:     "proof at word boundary",
			filename: "movie_proof.mkv",
			expected: false,
		},
		{
			name:     "sample as complete word",
			filename: "sample.mkv",
			expected: false,
		},
		{
			name:     "proof as complete word",
			filename: "proof.mkv",
			expected: false,
		},
		{
			name:     "sample at start of filename is rejected",
			filename: "samplemovie.mkv",
			expected: false,
		},
		{
			name:     "proof at start of filename is rejected",
			filename: "prooftest.mkv",
			expected: false,
		},
		{
			name:     "file without sample or proof passes",
			filename: "regular_movie.mkv",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAllowedFile(tt.filename, []string{})
			assert.Equal(t, tt.expected, result)
		})
	}
}
