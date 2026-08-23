package database

import "testing"

// TestBuildTitleLikePattern pins the LIKE wildcard conversion used by the
// healthy-file lookups. The pattern is intentionally word-order sensitive and
// must never degrade to a bare "%" (which would match every row) when handed
// an empty title.
func TestBuildTitleLikePattern(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"simple title", "Gladiator II", "%Gladiator%II%"},
		{"collapses whitespace", "  Movie   of  the   Year  ", "%Movie%of%the%Year%"},
		{"escapes LIKE wildcards", "Movie_100% (2024)", "%Movie\\_100\\%%(2024)%"},
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildTitleLikePattern(tc.in); got != tc.want {
				t.Fatalf("buildTitleLikePattern(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
