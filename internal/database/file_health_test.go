package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFileHealth_IsImported(t *testing.T) {
	tests := []struct {
		name        string
		filePath    string
		libraryPath *string
		want        bool
	}{
		{name: "nil library path", filePath: "tv/Show/S01E01.mkv", libraryPath: nil, want: false},
		{name: "empty library path", filePath: "tv/Show/S01E01.mkv", libraryPath: ptr(""), want: false},
		{name: "placeholder equal to file_path", filePath: "tv/Show/S01E01.mkv", libraryPath: ptr("tv/Show/S01E01.mkv"), want: false},
		{name: "placeholder with leading slash", filePath: "tv/Show/S01E01.mkv", libraryPath: ptr("/tv/Show/S01E01.mkv"), want: false},
		{name: "real linux library path", filePath: "tv/Show/S01E01.mkv", libraryPath: ptr("/mnt/data/tv/Show/S01E01.mkv"), want: true},
		{name: "real windows library path", filePath: "tv/Show/S01E01.mkv", libraryPath: ptr(`C:\media\tv\Show\S01E01.mkv`), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fh := &FileHealth{FilePath: tt.filePath, LibraryPath: tt.libraryPath}
			assert.Equal(t, tt.want, fh.IsImported())
		})
	}
}

func ptr(s string) *string {
	return &s
}

func TestFileHealth_EffectiveLibraryPath(t *testing.T) {
	t.Run("placeholder returns false", func(t *testing.T) {
		fh := &FileHealth{FilePath: "tv/Show/S01E01.mkv", LibraryPath: ptr("/tv/Show/S01E01.mkv")}
		p, ok := fh.EffectiveLibraryPath()
		assert.False(t, ok)
		assert.Equal(t, "", p)
	})

	t.Run("nil returns false", func(t *testing.T) {
		fh := &FileHealth{FilePath: "tv/Show/S01E01.mkv", LibraryPath: nil}
		p, ok := fh.EffectiveLibraryPath()
		assert.False(t, ok)
		assert.Equal(t, "", p)
	})

	t.Run("real path returns value", func(t *testing.T) {
		fh := &FileHealth{FilePath: "tv/Show/S01E01.mkv", LibraryPath: ptr("/mnt/data/tv/Show/S01E01.mkv")}
		p, ok := fh.EffectiveLibraryPath()
		assert.True(t, ok)
		assert.Equal(t, "/mnt/data/tv/Show/S01E01.mkv", p)
	})
}
