package sevenzip

import (
	"fmt"
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
)

// ---------------------------------------------------------------------------
// extractSevenZipPartNumber
// ---------------------------------------------------------------------------

func TestExtractSevenZipPartNumber(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     int
	}{
		// New format (.7z.NNN)
		{"new format .7z.001", "movie.7z.001", 1},
		{"new format .7z.002", "movie.7z.002", 2},
		{"new format .7z.016", "movie.7z.016", 16},
		{"new format .7z.068", "movie.7z.068", 68},
		{"new format uppercase", "MOVIE.7Z.003", 3},
		{"new format long name", "Puniru.is.a.Kawaii.Slime.2024.S01.7z.001", 1},

		// Old format first volume (.7z)
		{"old format .7z first vol", "movie.7z", 0},
		{"old format .7z uppercase", "MOVIE.7Z", 0},
		{"old format .7z long name", "Puniru.is.a.Kawaii.Slime.2024.S01.Ger.Sub.AAC.1080p.WEB-DL.H.264-HiSHiRO.7z", 0},

		// Old format subsequent volumes (plain numeric extension)
		{"old format .001", "movie.001", 1},
		{"old format .002", "movie.002", 2},
		{"old format .016", "movie.016", 16},
		{"old format .068", "movie.068", 68},
		{"old format long name .016", "Puniru.is.a.Kawaii.Slime.2024.S01.Ger.Sub.AAC.1080p.WEB-DL.H.264-HiSHiRO.016", 16},

		// Unknown / unsupported formats
		{"par2 file", "movie.7z.par2", 999999},
		{"rar file", "movie.part01.rar", 999999},
		{"mkv file", "movie.mkv", 999999},
		{"no extension", "movie", 999999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSevenZipPartNumber(tt.filename)
			if got != tt.want {
				t.Errorf("extractSevenZipPartNumber(%q) = %d; want %d", tt.filename, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// getPartSuffixSevenZip
// ---------------------------------------------------------------------------

func TestGetPartSuffixSevenZip(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		// New format — preserved as-is
		{"new format .7z.001", "movie.7z.001", ".7z.001"},
		{"new format .7z.002", "movie.7z.002", ".7z.002"},
		{"new format .7z.016", "movie.7z.016", ".7z.016"},
		{"new format .7z.068", "movie.7z.068", ".7z.068"},

		// Old format first volume → .7z.001
		{"old format .7z → .7z.001", "movie.7z", ".7z.001"},
		{"old format .7z uppercase", "MOVIE.7Z", ".7z.001"},
		{"old format .7z long name", "Puniru.is.a.Kawaii.Slime.2024.S01.Ger.Sub.AAC.1080p.WEB-DL.H.264-HiSHiRO.7z", ".7z.001"},

		// Old format subsequent volumes → .7z.NNN
		{"old format .002 → .7z.002", "movie.002", ".7z.002"},
		{"old format .016 → .7z.016", "movie.016", ".7z.016"},
		{"old format .068 → .7z.068", "movie.068", ".7z.068"},
		{"old format long name .016", "Puniru.is.a.Kawaii.Slime.2024.S01.Ger.Sub.AAC.1080p.WEB-DL.H.264-HiSHiRO.016", ".7z.016"},

		// Unknown extension — falls back to filepath.Ext
		{"unknown .mkv", "movie.mkv", ".mkv"},
		{"unknown .par2", "movie.par2", ".par2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getPartSuffixSevenZip(tt.filename)
			if got != tt.want {
				t.Errorf("getPartSuffixSevenZip(%q) = %q; want %q", tt.filename, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// renameSevenZipFilesAndSort — verifies the full rename+sort pipeline
// ---------------------------------------------------------------------------

func TestRenameSevenZipFilesAndSort_NewFormat(t *testing.T) {
	// Standard new format: name.7z.001 ... name.7z.005
	base := "Puniru.is.a.Kawaii.Slime.S01"
	files := []parser.ParsedFile{
		{Filename: base + ".7z.003", OriginalIndex: 2},
		{Filename: base + ".7z.001", OriginalIndex: 0},
		{Filename: base + ".7z.005", OriginalIndex: 4},
		{Filename: base + ".7z.002", OriginalIndex: 1},
		{Filename: base + ".7z.004", OriginalIndex: 3},
	}

	result := renameSevenZipFilesAndSort(files)

	if len(result) != 5 {
		t.Fatalf("expected 5 files, got %d", len(result))
	}

	expected := []string{
		base + ".7z.001",
		base + ".7z.002",
		base + ".7z.003",
		base + ".7z.004",
		base + ".7z.005",
	}
	for i, want := range expected {
		if result[i].Filename != want {
			t.Errorf("result[%d].Filename = %q; want %q", i, result[i].Filename, want)
		}
	}
}

func TestRenameSevenZipFilesAndSort_OldFormat(t *testing.T) {
	// Old format: name.7z (vol 1) + name.002 .. name.005 (vols 2-5)
	base := "Puniru.is.a.Kawaii.Slime.2024.S01.Ger.Sub.AAC.1080p.WEB-DL.H.264-HiSHiRO"
	files := []parser.ParsedFile{
		{Filename: base + ".016", OriginalIndex: 3},
		{Filename: base + ".7z", OriginalIndex: 0},
		{Filename: base + ".005", OriginalIndex: 2},
		{Filename: base + ".003", OriginalIndex: 1},
		{Filename: base + ".068", OriginalIndex: 4},
	}

	result := renameSevenZipFilesAndSort(files)

	if len(result) != 5 {
		t.Fatalf("expected 5 files, got %d", len(result))
	}

	// All must be renamed to .7z.NNN and sorted numerically
	expected := []string{
		base + ".7z.001", // was .7z
		base + ".7z.003", // was .003
		base + ".7z.005", // was .005
		base + ".7z.016", // was .016
		base + ".7z.068", // was .068
	}
	for i, want := range expected {
		if result[i].Filename != want {
			t.Errorf("result[%d].Filename = %q; want %q", i, result[i].Filename, want)
		}
	}
}

func TestRenameSevenZipFilesAndSort_OldFormat_68Parts(t *testing.T) {
	// Reproduce the real-world case: name.7z + name.002 ... name.068 (68 files total)
	base := "Puniru.is.a.Kawaii.Slime.2024.S01.Ger.Sub.AAC.1080p.WEB-DL.H.264-HiSHiRO"
	files := []parser.ParsedFile{
		{Filename: base + ".7z", OriginalIndex: 0},
	}
	for i := 2; i <= 68; i++ {
		files = append(files, parser.ParsedFile{
			Filename:      base + fmt.Sprintf(".%03d", i),
			OriginalIndex: i - 1,
		})
	}

	result := renameSevenZipFilesAndSort(files)

	if len(result) != 68 {
		t.Fatalf("expected 68 files, got %d", len(result))
	}

	// First file must be .7z.001 (renamed from .7z)
	if result[0].Filename != base+".7z.001" {
		t.Errorf("result[0].Filename = %q; want %q", result[0].Filename, base+".7z.001")
	}

	// Subsequent files must be .7z.002 .. .7z.068 in order
	for i := 2; i <= 68; i++ {
		want := base + fmt.Sprintf(".7z.%03d", i)
		if result[i-1].Filename != want {
			t.Errorf("result[%d].Filename = %q; want %q", i-1, result[i-1].Filename, want)
		}
	}
}

func TestRenameSevenZipFilesAndSort_SingleFile(t *testing.T) {
	// A single .7z file (non-split) must still work correctly.
	files := []parser.ParsedFile{
		{Filename: "movie.7z", OriginalIndex: 0},
	}

	result := renameSevenZipFilesAndSort(files)

	if len(result) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result))
	}
	if result[0].Filename != "movie.7z.001" {
		t.Errorf("result[0].Filename = %q; want %q", result[0].Filename, "movie.7z.001")
	}
}

// ---------------------------------------------------------------------------
// parseSevenZipFilename
// ---------------------------------------------------------------------------

func TestParseSevenZipFilename(t *testing.T) {
	sz := &sevenZipProcessor{}

	tests := []struct {
		name     string
		filename string
		wantBase string
		wantPart int
	}{
		// New format
		{"new format .001 → part 0", "movie.7z.001", "movie", 0},
		{"new format .002 → part 1", "movie.7z.002", "movie", 1},
		{"new format .016 → part 15", "movie.7z.016", "movie", 15},

		// Single .7z file
		{"single .7z → part 0", "movie.7z", "movie", 0},
		{"single .7z long name", "Show.S01.720p.7z", "Show.S01.720p", 0},

		// Unknown
		{"unknown extension → high part", "movie.mkv", "movie.mkv", 999999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBase, gotPart := sz.parseSevenZipFilename(tt.filename)
			if gotBase != tt.wantBase {
				t.Errorf("base = %q; want %q", gotBase, tt.wantBase)
			}
			if gotPart != tt.wantPart {
				t.Errorf("part = %d; want %d", gotPart, tt.wantPart)
			}
		})
	}
}
