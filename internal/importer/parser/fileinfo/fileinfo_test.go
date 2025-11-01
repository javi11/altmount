package fileinfo

import "testing"

func TestIsProbablyObfuscated(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		// Certainly obfuscated patterns
		{
			name:     "32 hex digits (MD5 hash)",
			filename: "b082fa0beaa644d3aa01045d5b8d0b36.mkv",
			want:     true,
		},
		{
			name:     "40+ hex digits and dots",
			filename: "0675e29e9abfd2.f7d069dab0b853283cc1b069a25f82.6547.mkv",
			want:     true,
		},
		{
			name:     "30+ hex with square brackets",
			filename: "[BlaBla] something 5937bc5e32146e.bef89a622e4a23f07b0d3757ad5e8a.a02b264e [More].mkv",
			want:     true,
		},
		{
			name:     "abc.xyz prefix",
			filename: "abc.xyz.a4c567edbcbf27.BLA.mkv",
			want:     true,
		},

		// Not obfuscated patterns
		{
			name:     "Great Distro - has uppercase, lowercase, space",
			filename: "Great Distro.mkv",
			want:     false,
		},
		{
			name:     "this is a download - multiple spaces",
			filename: "this is a download.mkv",
			want:     false,
		},
		{
			name:     "Beast 2020 - letters, digits, space",
			filename: "Beast 2020.mkv",
			want:     false,
		},
		{
			name:     "Catullus - starts with capital, mostly lowercase",
			filename: "Catullus.mkv",
			want:     false,
		},
		{
			name:     "Movie.Name.2023.1080p - typical release name",
			filename: "Movie.Name.2023.1080p.mkv",
			want:     false,
		},
		{
			name:     "The.Big.Movie.S01E01 - typical TV show",
			filename: "The.Big.Movie.S01E01.mkv",
			want:     false,
		},

		// Edge cases
		{
			name:     "Empty filename",
			filename: "",
			want:     false,
		},
		{
			name:     "Just extension",
			filename: ".mkv",
			want:     true, // No base filename, defaults to obfuscated
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isProbablyObfuscated(tt.filename)
			if got != tt.want {
				t.Errorf("isProbablyObfuscated(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestSelectBestFilename(t *testing.T) {
	tests := []struct {
		name            string
		par2Filename    string
		subjectFilename string
		headerFilename  string
		want            string
	}{
		{
			name:            "PAR2 wins over obfuscated subject",
			par2Filename:    "Movie.Name.2023.mkv",
			subjectFilename: "b082fa0beaa644d3aa01045d5b8d0b36.mkv",
			headerFilename:  "xyz123.mkv",
			want:            "Movie.Name.2023.mkv",
		},
		{
			name:            "Subject wins when PAR2 is obfuscated",
			par2Filename:    "abc.xyz.random123.mkv",
			subjectFilename: "Good.Movie.Name.mkv",
			headerFilename:  "header.mkv",
			want:            "Good.Movie.Name.mkv",
		},
		{
			name:            "Header wins when all others empty",
			par2Filename:    "",
			subjectFilename: "",
			headerFilename:  "Final.Name.mkv",
			want:            "Final.Name.mkv",
		},
		{
			name:            "Prefer important file type",
			par2Filename:    "",
			subjectFilename: "Movie.txt",
			headerFilename:  "Movie.mkv",
			want:            "Movie.mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectBestFilename(tt.par2Filename, tt.subjectFilename, tt.headerFilename)
			if got != tt.want {
				t.Errorf("selectBestFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}
