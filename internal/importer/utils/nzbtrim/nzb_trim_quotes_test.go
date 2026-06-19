package nzbtrim

import "testing"

func TestTrimSurroundingQuotes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single-quoted rar", "'movie.part001.rar'", "movie.part001.rar"},
		{"double-quoted rar", `"X.part01.rar"`, "X.part01.rar"},
		{"quoted obfuscated hash", "'a1b2c3d4.part01.rar'", "a1b2c3d4.part01.rar"},
		{"quoted no extension", "'Some.Show.S03'", "Some.Show.S03"},
		{"real-world quoted subject", "'Some.Show.S03.WEBRip.EAC3.2.0.1080p.x265-GRP.part001.rar'", "Some.Show.S03.WEBRip.EAC3.2.0.1080p.x265-GRP.part001.rar"},
		{"clean unchanged", "movie.part001.rar", "movie.part001.rar"},
		{"embedded apostrophe preserved", "It's.A.Wonderful.Life.part01.rar", "It's.A.Wonderful.Life.part01.rar"},
		{"surrounding whitespace trimmed", "  movie.rar  ", "movie.rar"},
		{"whitespace inside quotes trimmed", "' movie.rar '", "movie.rar"},
		{"nested pairs stripped", "''movie.rar''", "movie.rar"},
		{"mismatched quotes unchanged", `'movie.rar"`, `'movie.rar"`},
		{"empty", "", ""},
		{"single quote char", "'", "'"},
		{"only quotes", "''", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TrimSurroundingQuotes(tt.in)
			if got != tt.want {
				t.Errorf("TrimSurroundingQuotes(%q) = %q; want %q", tt.in, got, tt.want)
			}
			if again := TrimSurroundingQuotes(got); again != got {
				t.Errorf("not idempotent for %q: %q -> %q", tt.in, got, again)
			}
		})
	}
}
