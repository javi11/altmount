package rclonecli

import (
	"runtime"
	"strings"
	"testing"
)

func TestToVFSPath_ForwardSlashInputIsUnchanged(t *testing.T) {
	// The common case on every platform: callers already pass virtual paths, so
	// normalizing must not perturb them.
	cases := []string{
		"tv/Show.S01E01/ep.mkv",
		"tv/Show Name/file.mkv",
		"movies/Film.2024",
		"/tv/Show.S01E01",
		"/",
		".",
		"",
	}
	for _, in := range cases {
		if got := ToVFSPath(in); got != in {
			t.Errorf("ToVFSPath(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestToVFSPath_SeparatorHandlingIsPlatformCorrect(t *testing.T) {
	// Separator conversion is necessarily OS-specific. On Windows a backslash is
	// a path separator and has to become "/" or rclone matches nothing. On POSIX
	// it is a legal filename character and must be left exactly as-is.
	cases := []struct {
		in        string
		onWindows string
		onPosix   string
	}{
		{`tv\Show.S01E01\ep.mkv`, "tv/Show.S01E01/ep.mkv", `tv\Show.S01E01\ep.mkv`},
		{`\tv`, "/tv", `\tv`},
		{`\`, "/", `\`},
	}

	for _, tc := range cases {
		want := tc.onPosix
		if runtime.GOOS == "windows" {
			want = tc.onWindows
		}
		if got := ToVFSPath(tc.in); got != want {
			t.Errorf("ToVFSPath(%q) on %s = %q, want %q", tc.in, runtime.GOOS, got, want)
		}
	}
}

func TestToVFSPath_OutputNeverCarriesTheWindowsSeparator(t *testing.T) {
	// The property that actually matters at the RC boundary: whatever a caller
	// hands us, what reaches rclone must not contain an OS separator that rclone
	// will not understand. Only meaningful where '\' is the separator.
	if runtime.GOOS != "windows" {
		t.Skip("backslash is a legal filename character on POSIX, nothing to assert")
	}
	for _, in := range []string{`tv\Show\ep.mkv`, `\movies`, `\`, `movies/Mixed\Sep`} {
		if got := ToVFSPath(in); strings.Contains(got, `\`) {
			t.Errorf("ToVFSPath(%q) = %q, still contains a backslash separator", in, got)
		}
	}
}
