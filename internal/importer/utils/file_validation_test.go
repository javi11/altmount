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
			filename: "movie.sample.mkv",
			expected: false,
		},
		{
			name:     "empty extensions allows file with merged sample",
			filename: "samplemovie.mkv",
			expected: true,
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
		{
			name:     "large file with sample name is allowed",
			filename: "movie.sample.mkv",
			expected: true,
		},
		{
			name:     "sample subtitle file is allowed",
			filename: "movie.sample.srt",
			expected: true,
		},
		{
			name:     "sample image file is allowed",
			filename: "proof.jpg",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := int64(0)
			if tt.name == "large file with sample name is allowed" {
				size = 201 * 1024 * 1024 // > 200MB
			}
			result := IsAllowedFile(tt.filename, size, []string{})
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsAllowedFile_WithExtensions(t *testing.T) {
	allowedExts := []string{".mkv", ".mp4", ".srt"}

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
			name:     "subtitle sample file is allowed",
			filename: "movie.sample.srt",
			expected: true,
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
		{
			name:     "allowed extension without dot",
			filename: "movie.mkv",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAllowedFile(tt.filename, 0, allowedExts)
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
				{Filename: "movie.mkv", Size: 500 * 1024 * 1024},
				{Filename: "video.mp4", Size: 500 * 1024 * 1024},
			},
			expected: true,
		},
		{
			name: "empty extensions rejects only sample files",
			files: []parser.ParsedFile{
				{Filename: "sample.movie.mkv", Size: 10 * 1024 * 1024},
				{Filename: "proof.video.mp4", Size: 10 * 1024 * 1024},
			},
			expected: false,
		},
		{
			name: "empty extensions allows at least one non-sample",
			files: []parser.ParsedFile{
				{Filename: "movie.mkv", Size: 500 * 1024 * 1024},
				{Filename: "sample.movie.mkv", Size: 10 * 1024 * 1024},
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
				{Filename: "movie.mkv", Size: 500 * 1024 * 1024},
				{Filename: "video.mp4", Size: 500 * 1024 * 1024},
			},
			allowed:  []string{".mkv"},
			expected: true,
		},
		{
			name: "no matching files",
			files: []parser.ParsedFile{
				{Filename: "movie.avi", Size: 500 * 1024 * 1024},
				{Filename: "video.wmv", Size: 500 * 1024 * 1024},
			},
			allowed:  []string{".mkv", ".mp4"},
			expected: false,
		},
		{
			name: "sample files are filtered out",
			files: []parser.ParsedFile{
				{Filename: "movie.sample.mkv", Size: 10 * 1024 * 1024},
				{Filename: "video.proof.mkv", Size: 10 * 1024 * 1024},
			},
			allowed:  []string{".mkv"},
			expected: false,
		},
		{
			name: "mixed files with at least one valid",
			files: []parser.ParsedFile{
				{Filename: "movie.mkv", Size: 500 * 1024 * 1024},
				{Filename: "video.avi", Size: 500 * 1024 * 1024},
				{Filename: "sample.mkv", Size: 10 * 1024 * 1024},
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
			name:     "sample at dot boundary",
			filename: "movie.sample.mkv",
			expected: false,
		},
		{
			name:     "proof at dash boundary",
			filename: "movie-proof.mkv",
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
			name:     "sample at start of filename with dot is rejected",
			filename: "sample.movie.mkv",
			expected: false,
		},
		{
			name:     "proof at start of filename with underscore is allowed (word char)",
			filename: "proof_test.mkv",
			expected: true,
		},
		{
			name:     "sample merged with word is allowed",
			filename: "samplemovie.mkv",
			expected: true,
		},
		{
			name:     "proof merged with word is allowed",
			filename: "prooftest.mkv",
			expected: true,
		},
		{
			name:     "title with Proof is rejected",
			filename: "Bye.Bye.Earth.S01E04.Spell.of.Proof.Still.Distant.1080p.mkv",
			expected: false,
		},
		{
			name:     "title with of proof is rejected",
			filename: "the.spell.of.proof.mkv",
			expected: false,
		},
		{
			name:     "title with Samples is allowed",
			filename: "SpongeBob.SquarePants.S08E18.Free.Samples.mkv",
			expected: true,
		},
		{
			name:     "file without sample or proof passes",
			filename: "regular_movie.mkv",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAllowedFile(tt.filename, 0, []string{})
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsAllowedFile_MixedDotFormats(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		allowed     []string
		expected    bool
	}{
		{
			name:     "allowed without dot matches file",
			filename: "movie.mkv",
			allowed:  []string{"mkv"},
			expected: true,
		},
		{
			name:     "allowed with dot matches file",
			filename: "movie.mkv",
			allowed:  []string{".mkv"},
			expected: true,
		},
		{
			name:     "mixed allowed list",
			filename: "movie.mp4",
			allowed:  []string{"mkv", ".mp4"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAllowedFile(tt.filename, 0, tt.allowed)
			assert.Equal(t, tt.expected, result)
		})
	}
}
