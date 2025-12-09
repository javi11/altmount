package nzbfilesystem

import (
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
			if got != tt.expected {
				t.Errorf("normalizePath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
