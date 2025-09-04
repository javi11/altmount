package importer

import (
	"bytes"
	"testing"

	"github.com/javi11/nzbparser"
)

func TestDeobfuscator_IsDeobfuscationWorthwhile(t *testing.T) {
	deobfuscator := NewDeobfuscator(nil)

	tests := []struct {
		name     string
		filename string
		allFiles []nzbparser.NzbFile
		want     bool
	}{
		{
			name:     "clean filename - not worthwhile",
			filename: "Movie.Title.2023.1080p.BluRay.x264.mkv",
			allFiles: []nzbparser.NzbFile{},
			want:     false,
		},
		{
			name:     "obfuscated filename with PAR2 - worthwhile",
			filename: "abc123def456ghi789jkl012mno345pqr678.mkv",
			allFiles: []nzbparser.NzbFile{
				{Filename: "repair.par2"},
			},
			want: true,
		},
		{
			name:     "32-char hex filename - worthwhile",
			filename: "a1b2c3d4e5f6789012345678901234ab.mkv",
			allFiles: []nzbparser.NzbFile{},
			want:     true,
		},
		{
			name:     "abc.xyz prefix - worthwhile",
			filename: "abc.xyz.random.string.mkv",
			allFiles: []nzbparser.NzbFile{},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deobfuscator.IsDeobfuscationWorthwhile(tt.filename, tt.allFiles)
			if got != tt.want {
				t.Errorf("IsDeobfuscationWorthwhile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeobfuscator_cleanObfuscatedFilename(t *testing.T) {
	deobfuscator := NewDeobfuscator(nil)

	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{
			name:     "remove abc.xyz prefix",
			filename: "abc.xyz.movie.title.mkv",
			want:     "movie.title.mkv",
		},
		{
			name:     "remove bracket patterns",
			filename: "movie[OBFUSCATED]title[RANDOM].mkv",
			want:     "movietitle.mkv",
		},
		{
			name:     "clean multiple dots",
			filename: "movie..title...2023.mkv",
			want:     "movie.title.2023.mkv",
		},
		{
			name:     "trim artifacts",
			filename: "..movie.title.-.mkv",
			want:     "movie.title.mkv",
		},
		{
			name:     "no change needed",
			filename: "clean.filename.mkv",
			want:     "clean.filename.mkv",
		},
		{
			name:     "extension handling",
			filename: "abc.xyz.obfuscated.mp4",
			want:     "obfuscated.mp4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deobfuscator.cleanObfuscatedFilename(tt.filename)
			if got != tt.want {
				t.Errorf("cleanObfuscatedFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeobfuscator_extractMeaningfulParts(t *testing.T) {
	deobfuscator := NewDeobfuscator(nil)

	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{
			name:     "extract meaningful parts",
			filename: "movie.title.a1b2c3d4e5f6789012345678901234ab.2023",
			want:     "movie.title.2023",
		},
		{
			name:     "skip short parts",
			filename: "a.bb.movie.title.x.y",
			want:     "movie.title",
		},
		{
			name:     "skip hex patterns",
			filename: "movie.title.deadbeefcafebabe12345678abcdef12.mkv",
			want:     "movie.title.mkv",
		},
		{
			name:     "handle random long strings",
			filename: "movie.abcdefghijklmnopqrstuvwxyz123456.title",
			want:     "movie.title",
		},
		{
			name:     "no meaningful parts",
			filename: "a1b2c3d4e5f6789012345678901234ab",
			want:     "a1b2c3d4e5f6789012345678901234ab",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deobfuscator.extractMeaningfulParts(tt.filename)
			if got != tt.want {
				t.Errorf("extractMeaningfulParts() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeobfuscator_inferFromPar2Filename(t *testing.T) {
	deobfuscator := NewDeobfuscator(nil)

	tests := []struct {
		name           string
		par2Filename   string
		targetFilename string
		want           string
	}{
		{
			name:           "basic par2 pattern",
			par2Filename:   "Movie.Title.2023.par2",
			targetFilename: "a1b2c3d4e5f6789012345678901234ab.mkv",
			want:           "Movie.title.2023.mkv",
		},
		{
			name:           "volume par2 pattern",
			par2Filename:   "Movie.Title.2023.vol01+02.par2",
			targetFilename: "obfuscated123.mkv",
			want:           "Movie.title.2023.mkv",
		},
		{
			name:           "obfuscated par2 - no result",
			par2Filename:   "a1b2c3d4e5f6789012345678901234ab.par2",
			targetFilename: "target.mkv",
			want:           "",
		},
		{
			name:           "same as target - no result",
			par2Filename:   "target.par2",
			targetFilename: "target.mkv",
			want:           "",
		},
		{
			name:           "short name - no result",
			par2Filename:   "ab.par2",
			targetFilename: "target.mkv",
			want:           "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deobfuscator.inferFromPar2Filename(tt.par2Filename, tt.targetFilename)
			if got != tt.want {
				t.Errorf("inferFromPar2Filename() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeobfuscator_improveFilename(t *testing.T) {
	deobfuscator := NewDeobfuscator(nil)

	tests := []struct {
		name             string
		baseName         string
		originalFilename string
		want             string
	}{
		{
			name:             "basic improvement",
			baseName:         "movie.title.2023",
			originalFilename: "obfuscated.mkv",
			want:             "Movie.title.2023.mkv",
		},
		{
			name:             "with existing extension",
			baseName:         "movie.title",
			originalFilename: "obfuscated.mp4",
			want:             "Movie.title.mp4",
		},
		{
			name:             "no extension in original",
			baseName:         "movie.title",
			originalFilename: "obfuscated",
			want:             "Movie.title",
		},
		{
			name:             "extension already present",
			baseName:         "movie.title.mkv",
			originalFilename: "obfuscated.mkv",
			want:             "Movie.title.mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deobfuscator.improveFilename(tt.baseName, tt.originalFilename)
			if got != tt.want {
				t.Errorf("improveFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeobfuscator_DeobfuscateFilename(t *testing.T) {
	deobfuscator := NewDeobfuscator(nil) // No pool manager for these tests

	tests := []struct {
		name        string
		filename    string
		allFiles    []nzbparser.NzbFile
		wantSuccess bool
		wantMethod  string
	}{
		{
			name:        "clean filename - no deobfuscation needed",
			filename:    "Movie.Title.2023.mkv",
			allFiles:    []nzbparser.NzbFile{},
			wantSuccess: false,
			wantMethod:  "none",
		},
		{
			name:     "par2 available for obfuscated file",
			filename: "a1b2c3d4e5f6789012345678901234ab.mkv",
			allFiles: []nzbparser.NzbFile{
				{Filename: "Movie.Title.2023.par2"},
			},
			wantSuccess: true,
			wantMethod:  "par2_extraction",
		},
		{
			name:        "pattern cleanup success",
			filename:    "abc.xyz.movie.title.mkv",
			allFiles:    []nzbparser.NzbFile{},
			wantSuccess: true,
			wantMethod:  "pattern_cleanup",
		},
		{
			name:        "bracket removal",
			filename:    "movie[OBFUSCATED]title.mkv",
			allFiles:    []nzbparser.NzbFile{},
			wantSuccess: true,
			wantMethod:  "pattern_cleanup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a dummy current file for the test
			currentFile := nzbparser.NzbFile{
				Filename: tt.filename,
				Subject:  tt.filename,
			}

			result := deobfuscator.DeobfuscateFilename(tt.filename, tt.allFiles, currentFile)

			if result.Success != tt.wantSuccess {
				t.Errorf("DeobfuscateFilename() success = %v, want %v", result.Success, tt.wantSuccess)
			}

			if result.Method != tt.wantMethod {
				t.Errorf("DeobfuscateFilename() method = %v, want %v", result.Method, tt.wantMethod)
			}

			if result.OriginalFilename != tt.filename {
				t.Errorf("DeobfuscateFilename() original = %v, want %v", result.OriginalFilename, tt.filename)
			}

			// If successful, the deobfuscated filename should be different and less obfuscated
			if result.Success && result.DeobfuscatedFilename == tt.filename {
				t.Errorf("DeobfuscateFilename() should produce different result when successful")
			}
		})
	}
}

func TestDeobfuscator_extractFromPar2Files(t *testing.T) {
	deobfuscator := NewDeobfuscator(nil) // No pool manager

	tests := []struct {
		name           string
		allFiles       []nzbparser.NzbFile
		targetFilename string
		want           string
	}{
		{
			name: "par2 file with clean name",
			allFiles: []nzbparser.NzbFile{
				{Filename: "Movie.Title.2023.par2"},
				{Filename: "obfuscated123.mkv"},
			},
			targetFilename: "obfuscated123.mkv",
			want:           "Movie.title.2023.mkv",
		},
		{
			name: "volume par2 file",
			allFiles: []nzbparser.NzbFile{
				{Filename: "Movie.Title.vol01+02.par2"},
				{Filename: "random456.mkv"},
			},
			targetFilename: "random456.mkv",
			want:           "Movie.title.mkv",
		},
		{
			name: "no par2 files",
			allFiles: []nzbparser.NzbFile{
				{Filename: "movie.mkv"},
			},
			targetFilename: "movie.mkv",
			want:           "",
		},
		{
			name: "obfuscated par2 file",
			allFiles: []nzbparser.NzbFile{
				{Filename: "a1b2c3d4e5f6789012345678901234ab.par2"},
				{Filename: "target.mkv"},
			},
			targetFilename: "target.mkv",
			want:           "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deobfuscator.extractFromPar2Files(tt.allFiles, tt.targetFilename)
			if got != tt.want {
				t.Errorf("extractFromPar2Files() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeobfuscator_parsePAR2Header(t *testing.T) {
	deobfuscator := NewDeobfuscator(nil)

	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name: "valid PAR2 header",
			data: []byte{
				// Magic: "PAR2\0PKT"
				'P', 'A', 'R', '2', 0, 'P', 'K', 'T',
				// Length: 100 (little endian)
				100, 0, 0, 0, 0, 0, 0, 0,
				// MD5 hash (16 bytes)
				1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
				// Recovery ID (16 bytes)
				1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
				// Type (16 bytes)
				'P', 'A', 'R', ' ', '2', '.', '0', 0, 'F', 'i', 'l', 'e', 'D', 'e', 's', 'c',
			},
			wantErr: false,
		},
		{
			name: "invalid magic signature",
			data: []byte{
				// Invalid magic
				'B', 'A', 'D', '!', 0, 'P', 'K', 'T',
				// Rest of header...
				100, 0, 0, 0, 0, 0, 0, 0,
				1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
				1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
				'P', 'A', 'R', ' ', '2', '.', '0', 0, 'F', 'i', 'l', 'e', 'D', 'e', 's', 'c',
			},
			wantErr: true,
		},
		{
			name:    "too short data",
			data:    []byte{1, 2, 3, 4},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader(tt.data)
			header, err := deobfuscator.parsePAR2Header(r)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parsePAR2Header() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("parsePAR2Header() unexpected error: %v", err)
				return
			}

			if header == nil {
				t.Errorf("parsePAR2Header() returned nil header")
				return
			}

			// Validate magic signature
			expectedMagic := [8]byte{'P', 'A', 'R', '2', 0, 'P', 'K', 'T'}
			if header.Magic != expectedMagic {
				t.Errorf("parsePAR2Header() magic = %v, want %v", header.Magic, expectedMagic)
			}

			// Validate length
			if header.Length != 100 {
				t.Errorf("parsePAR2Header() length = %d, want 100", header.Length)
			}
		})
	}
}
