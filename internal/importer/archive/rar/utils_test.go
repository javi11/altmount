package rar

import (
	"fmt"
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
		{"old roll rollover s00", "movie.s00", "movie", true},
		{"old roll rollover s12", "movie.s12", "movie", true},
		{"old roll rollover z99", "movie.z99", "movie", true},
		{"old roll rollover uppercase", "Movie.Name.S00", "movie.name", true},
		{"numeric", "archive.001", "archive", true},
		{"7z numeric", "archive.7z.001", "archive.7z", true},
		{"strips directory", "sub/dir/movie.part02.rar", "movie", true},
		{"plain media file", "movie.mkv", "", false},
		{"no extension obfuscated", "a1b2c3d4e5", "", false},
		{"par2", "movie.par2", "", false},
		{"embedded apostrophe not surrounding quotes", "It's.A.Wonderful.Life.part01.rar", "it's.a.wonderful.life", true},
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

// TestExtractRarBaseName covers both branches: a recognized volume resolves to its
// SetKey, and an unrecognized name falls back to the full lowercased base so it only
// groups with itself. Inputs are assumed canonical (quotes stripped upstream).
func TestExtractRarBaseName(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"recognized volume 3-digit", "movie.part001.rar", "movie"},
		{"recognized volume strips directory", "sub/dir/movie.part02.rar", "movie"},
		{"embedded apostrophe preserved", "It's.A.Wonderful.Life.part01.rar", "it's.a.wonderful.life"},
		{"non-volume falls back to full name", "movie.mkv", "movie.mkv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRarBaseName(tt.filename)
			if got != tt.want {
				t.Errorf("extractRarBaseName(%q) = %q; want %q", tt.filename, got, tt.want)
			}
		})
	}
}

// TestGroupArchivesByBaseNamePaddedWidth proves multi-volume parts collapse into a
// single group regardless of zero-padding width (part001 vs part01). Inputs are
// canonical; poster quotes are stripped upstream at the nzbparser boundary.
func TestGroupArchivesByBaseNamePaddedWidth(t *testing.T) {
	makeFiles := func(names ...string) []parser.ParsedFile {
		out := make([]parser.ParsedFile, len(names))
		for i, n := range names {
			out[i] = parser.ParsedFile{Filename: n}
		}
		return out
	}

	tests := []struct {
		name       string
		files      []parser.ParsedFile
		wantGroups int
		wantParts  int
	}{
		{
			name: "3-digit padding collapses to 1 group of 5",
			files: makeFiles(
				"movie.part001.rar",
				"movie.part002.rar",
				"movie.part003.rar",
				"movie.part004.rar",
				"movie.part005.rar",
			),
			wantGroups: 1,
			wantParts:  5,
		},
		{
			name: "2-digit padding collapses to 1 group of 3",
			files: makeFiles(
				"movie.part01.rar",
				"movie.part02.rar",
				"movie.part03.rar",
			),
			wantGroups: 1,
			wantParts:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := GroupArchivesByBaseName(tt.files)
			if len(groups) != tt.wantGroups {
				t.Errorf("GroupArchivesByBaseName() got %d groups; want %d (got groups: %v)",
					len(groups), tt.wantGroups, groupNames(groups))
			}
			if tt.wantGroups == 1 && len(groups) == 1 && len(groups[0]) != tt.wantParts {
				t.Errorf("GroupArchivesByBaseName() group[0] has %d parts; want %d",
					len(groups[0]), tt.wantParts)
			}
		})
	}
}

func groupNames(groups [][]parser.ParsedFile) [][]string {
	out := make([][]string, len(groups))
	for i, g := range groups {
		names := make([]string, len(g))
		for j, f := range g {
			names[j] = f.Filename
		}
		out[i] = names
	}
	return out
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
		{"old roll full rollover into s contiguous", files(oldRollSet("m", 12)...), false},
		{"old roll rollover missing s00", files(oldRollSetSkip("m", 12, "m.s00")...), true},
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

func TestRarVolumeNumber(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		wantScheme rarScheme
		wantNum    int
		wantOK     bool
	}{
		{"first volume .rar", "movie.rar", schemeRoll, 0, true},
		{"old roll r00", "movie.r00", schemeRoll, 1, true},
		{"old roll r99", "movie.r99", schemeRoll, 100, true},
		// Old-style naming rolls .r99 -> .s00, so s00 must be the volume right after
		// r99 (contiguous ordinal) — otherwise gap detection misfires.
		{"old roll rollover s00", "movie.s00", schemeRoll, 101, true},
		{"old roll rollover s12", "movie.s12", schemeRoll, 113, true},
		{"old roll rollover z99", "movie.z99", schemeRoll, 900, true},
		{"old roll rollover uppercase", "Movie.S00", schemeRoll, 101, true},
		{"part scheme", "movie.part02.rar", schemePart, 2, true},
		{"numeric scheme", "archive.003", schemeNumeric, 3, true},
		{"not a volume", "movie.mkv", schemeUnknown, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, n, ok := rarVolumeNumber(tt.filename)
			if s != tt.wantScheme || n != tt.wantNum || ok != tt.wantOK {
				t.Errorf("rarVolumeNumber(%q) = (%v, %d, %t); want (%v, %d, %t)",
					tt.filename, s, n, ok, tt.wantScheme, tt.wantNum, tt.wantOK)
			}
		})
	}
}

// oldRollSet returns a full old-style multi-volume RAR set name list:
// base.rar, base.r00..base.r99, base.s00..base.s{sCount}.
func oldRollSet(base string, sCount int) []string {
	names := []string{base + ".rar"}
	for i := 0; i <= 99; i++ {
		names = append(names, fmt.Sprintf("%s.r%02d", base, i))
	}
	for i := 0; i <= sCount; i++ {
		names = append(names, fmt.Sprintf("%s.s%02d", base, i))
	}
	return names
}

// oldRollSetSkip returns oldRollSet with the named volume removed, simulating a gap.
func oldRollSetSkip(base string, sCount int, skip string) []string {
	all := oldRollSet(base, sCount)
	out := all[:0:0]
	for _, n := range all {
		if n != skip {
			out = append(out, n)
		}
	}
	return out
}
