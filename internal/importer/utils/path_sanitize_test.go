package utils

import "testing"

func TestSanitizePathSegment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"simple category", "movies", "movies"},
		{"nested category", "movies/4k", "movies/4k"},
		{"leading and trailing slashes trimmed", "/movies/", "movies"},
		{"backslash normalized to forward slash", `movies\4k`, "movies/4k"},
		{"bare traversal segment rejected", "..", ""},
		{"bare dot segment rejected", ".", ""},
		{"leading traversal rejected", "../../etc", ""},
		{"trailing traversal rejected", "movies/..", ""},
		{"embedded traversal rejected", "movies/../../etc", ""},
		{"backslash traversal rejected", `..\..\etc`, ""},
		{"mixed slash traversal rejected", `movies\../etc`, ""},
		{"double slash rejected as empty segment", "movies//4k", ""},
		{"traversal-looking but safe filename preserved", "movies..2024", "movies..2024"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SanitizePathSegment(tt.input); got != tt.want {
				t.Errorf("SanitizePathSegment(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}
