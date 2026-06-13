package fileinfo

import "testing"

func TestIsRarFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"plain rar", "movie.rar", true},
		{"old roll r00", "movie.r00", true},
		{"old roll r99", "movie.r99", true},
		// Old-style naming rolls .r99 -> .s00 -> ... -> .z99; these continuation
		// volumes must be recognized as RAR parts or they get dropped from the set.
		{"old roll rollover s00", "movie.s00", true},
		{"old roll rollover s12", "movie.s12", true},
		{"old roll rollover z99", "movie.z99", true},
		{"part rar", "movie.part01.rar", true},
		{"uppercase rollover", "MOVIE.S00", true},
		{"plain media file", "movie.mkv", false},
		{"par2", "movie.par2", false},
		{"nfo", "movie.nfo", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRarFile(tt.filename); got != tt.want {
				t.Errorf("IsRarFile(%q) = %t; want %t", tt.filename, got, tt.want)
			}
		})
	}
}
