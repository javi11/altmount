package rar

import (
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
)

func TestSetKey(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantKey  string
		wantOK   bool
	}{
		{"part rar", "movie.part01.rar", "movie", true},
		{"part rar padded", "Movie.Name.PART001.RAR", "movie.name", true},
		{"plain rar", "movie.rar", "movie", true},
		{"old roll r00", "movie.r00", "movie", true},
		{"old roll r15", "movie.r15", "movie", true},
		{"numeric", "archive.001", "archive", true},
		{"7z numeric", "archive.7z.001", "archive.7z", true},
		{"strips directory", "sub/dir/movie.part02.rar", "movie", true},
		{"plain media file", "movie.mkv", "", false},
		{"no extension obfuscated", "a1b2c3d4e5", "", false},
		{"par2", "movie.par2", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotOK := SetKey(tt.filename)
			if gotKey != tt.wantKey || gotOK != tt.wantOK {
				t.Errorf("SetKey(%q) = (%q, %t); want (%q, %t)",
					tt.filename, gotKey, gotOK, tt.wantKey, tt.wantOK)
			}
		})
	}
}

func TestGroupHasVolumeGap(t *testing.T) {
	files := func(names ...string) []parser.ParsedFile {
		out := make([]parser.ParsedFile, len(names))
		for i, n := range names {
			out[i] = parser.ParsedFile{Filename: n}
		}
		return out
	}

	tests := []struct {
		name  string
		files []parser.ParsedFile
		want  bool
	}{
		{"single rar", files("movie.rar"), false},
		{"contiguous part set", files("m.part01.rar", "m.part02.rar", "m.part03.rar"), false},
		{"part set middle gap", files("m.part01.rar", "m.part03.rar"), true},
		{"part set missing first", files("m.part02.rar", "m.part03.rar"), true},
		{"old roll contiguous", files("m.rar", "m.r00", "m.r01"), false},
		{"old roll missing first volume", files("m.r00", "m.r01"), true},
		{"old roll interior gap", files("m.rar", "m.r00", "m.r02"), true},
		{"numeric contiguous", files("a.001", "a.002", "a.003"), false},
		{"numeric gap", files("a.001", "a.003"), true},
		{"numeric missing first", files("a.002", "a.003"), true},
		{"mixed padding contiguous", files("m.part1.rar", "m.part02.rar"), false},
		{"mixed schemes not flagged", files("m.rar", "m.part02.rar"), false},
		{"unrecognized member not flagged", files("m.r00", "obfuscated"), false},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := groupHasVolumeGap(tt.files); got != tt.want {
				t.Errorf("groupHasVolumeGap(%v) = %t; want %t", tt.files, got, tt.want)
			}
		})
	}
}
