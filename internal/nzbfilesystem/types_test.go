package nzbfilesystem

import (
	"path/filepath"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty path",
			input:    "",
			expected: "/",
		},
		{
			name:     "root path",
			input:    "/",
			expected: "/",
		},
		{
			name:     "clean path",
			input:    "/foo/bar",
			expected: "/foo/bar",
		},
		{
			name:     "trailing slash",
			input:    "/foo/bar/",
			expected: "/foo/bar",
		},
		{
			name:     "multiple trailing slashes",
			input:    "/foo/bar//",
			expected: "/foo/bar",
		},
		{
			name:     "backslash",
			input:    `\foo\bar`,
			expected: "/foo/bar",
		},
		{
			name:     "mixed slashes",
			input:    `/foo\bar/`,
			expected: "/foo/bar",
		},
		{
			name:     "trailing backslash",
			input:    `/foo/bar\`,
			expected: "/foo/bar",
		},
		{
			name:     "dot path",
			input:    ".",
			expected: "/",
		},
		{
			name:     "relative path",
			input:    "foo/bar",
			expected: "foo/bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePath(tt.input)
			// compare normalized forms using forward slashes so tests pass on Windows and Unix
			gotNorm := filepath.ToSlash(got)
			expNorm := filepath.ToSlash(tt.expected)
			if gotNorm != expNorm {
				t.Errorf("normalizePath(%q) = %q, want %q", tt.input, gotNorm, expNorm)
			}
		})
	}
}
