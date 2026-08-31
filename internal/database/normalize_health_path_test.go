package database

import "testing"

// TestNormalizeHealthPath pins the canonical form every file_path writer must
// produce: backslashes converted to forward slashes BEFORE the leading-slash
// trim, so a Windows path that arrives with a leading backslash (e.g. after a
// library-dir prefix is stripped from C:\rclone\tv\...) collapses to the same
// key as the import-time forward-slash path instead of splitting into a second
// row. Doing the trim first would leave a stray leading slash ("/tv/..."),
// which is the bug this guards against.
func TestNormalizeHealthPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"already canonical", "tv/Show/S01E01.mkv", "tv/Show/S01E01.mkv"},
		{"leading slash", "/tv/Show/S01E01.mkv", "tv/Show/S01E01.mkv"},
		{"doubled leading slash", "//tv/Show/S01E01.mkv", "tv/Show/S01E01.mkv"},
		{"internal backslashes", `tv\Show\S01E01.mkv`, "tv/Show/S01E01.mkv"},
		{"leading backslash", `\tv\Show\S01E01.mkv`, "tv/Show/S01E01.mkv"},
		{"absolute windows path", `C:\rclone\tv\S01E01.mkv`, "C:/rclone/tv/S01E01.mkv"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeHealthPath(c.in); got != c.want {
				t.Errorf("normalizeHealthPath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
