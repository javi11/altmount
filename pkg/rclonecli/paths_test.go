package rclonecli

import (
	"os"
	"strings"
	"testing"
)

// toSlashSep is driven with an explicit separator so the Windows behaviour this
// exists for is asserted on every platform, including a Linux CI runner where
// ToVFSPath itself is a no-op.
func TestToSlashSep_WindowsSeparator(t *testing.T) {
	cases := []struct{ in, want string }{
		{`tv\Show.S01E01\ep.mkv`, "tv/Show.S01E01/ep.mkv"},
		{`\tv`, "/tv"},
		{`\`, "/"},
		{`movies\Film.2024`, "movies/Film.2024"},
		{`movies/Already/Slashed`, "movies/Already/Slashed"},
		{`mixed\sep/path\here`, "mixed/sep/path/here"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := toSlashSep(tc.in, '\\'); got != tc.want {
			t.Errorf("toSlashSep(%q, '\\\\') = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestToSlashSep_PosixSeparatorIsAPassThrough(t *testing.T) {
	// Where '/' is already the separator, nothing may be rewritten. A backslash
	// is a legal character in a POSIX filename and must survive untouched.
	cases := []string{
		"tv/Show.S01E01/ep.mkv",
		`a\backslash\in\a\posix\name`,
		"/",
		"",
	}
	for _, in := range cases {
		if got := toSlashSep(in, '/'); got != in {
			t.Errorf("toSlashSep(%q, '/') = %q, want it unchanged", in, got)
		}
	}
}

func TestToSlashSep_OutputNeverCarriesTheSeparator(t *testing.T) {
	// The property that actually matters at the RC boundary.
	for _, in := range []string{`tv\Show\ep.mkv`, `\movies`, `\`, `movies/Mixed\Sep`} {
		if got := toSlashSep(in, '\\'); strings.Contains(got, `\`) {
			t.Errorf("toSlashSep(%q, '\\\\') = %q, still contains a separator", in, got)
		}
	}
}

func TestToVFSPath_MatchesTheHostSeparator(t *testing.T) {
	// ToVFSPath is toSlashSep bound to the running OS, so it is a no-op on POSIX
	// and rewrites on Windows. Asserting it against os.PathSeparator keeps the
	// binding honest without duplicating the table above.
	in := `tv\Show` + string(os.PathSeparator) + "ep.mkv"
	if got, want := ToVFSPath(in), toSlashSep(in, os.PathSeparator); got != want {
		t.Errorf("ToVFSPath(%q) = %q, want %q", in, got, want)
	}

	// Forward-slash input is unchanged everywhere: the common case, since callers
	// are meant to be handing over virtual paths already.
	for _, s := range []string{"tv/Show/ep.mkv", "movies/Film.2024", "/", ".", ""} {
		if got := ToVFSPath(s); got != s {
			t.Errorf("ToVFSPath(%q) = %q, want it unchanged", s, got)
		}
	}
}

func TestToVFSPaths(t *testing.T) {
	in := []string{"tv/Show", "movies/Film"}
	got := ToVFSPaths(in)
	if len(got) != len(in) {
		t.Fatalf("ToVFSPaths returned %d entries, want %d", len(got), len(in))
	}
	for i := range in {
		if got[i] != ToVFSPath(in[i]) {
			t.Errorf("ToVFSPaths[%d] = %q, want %q", i, got[i], ToVFSPath(in[i]))
		}
	}

	// Must not alias or mutate the caller's slice.
	if &got[0] == &in[0] {
		t.Error("ToVFSPaths returned the input slice rather than a new one")
	}
	if empty := ToVFSPaths(nil); len(empty) != 0 {
		t.Errorf("ToVFSPaths(nil) = %q, want empty", empty)
	}
}
