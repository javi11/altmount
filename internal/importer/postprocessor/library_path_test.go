package postprocessor

import "testing"

func TestBuildLibraryRelPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		relPath     string
		completeDir string
		category    string
		want        string
	}{
		{
			name:        "forward slash with both prefixes already present",
			relPath:     "complete/Movies/Moviefolder/file.mkv",
			completeDir: "/complete",
			category:    "Movies",
			want:        "complete/Movies/Moviefolder/file.mkv",
		},
		{
			// Regression test for issue #585 Bug A: Windows-style backslash
			// input from filepath.Rel must be normalised before stripping
			// or the category/complete prefix gets doubled.
			name:        "backslash input (Windows shape) with both prefixes",
			relPath:     `complete\Movies\Moviefolder\file.mkv`,
			completeDir: "/complete",
			category:    "Movies",
			want:        "complete/Movies/Moviefolder/file.mkv",
		},
		{
			name:        "backslash input, empty completeDir, category present",
			relPath:     `Movies\Moviefolder\file.mkv`,
			completeDir: "",
			category:    "Movies",
			want:        "Movies/Moviefolder/file.mkv",
		},
		{
			name:        "category missing from relPath gets injected once",
			relPath:     "Moviefolder/file.mkv",
			completeDir: "",
			category:    "Movies",
			want:        "Movies/Moviefolder/file.mkv",
		},
		{
			name:        "complete and category both missing get injected once",
			relPath:     "Moviefolder/file.mkv",
			completeDir: "/complete",
			category:    "Movies",
			want:        "complete/Movies/Moviefolder/file.mkv",
		},
		{
			name:        "both prefixes empty, slash-normalises only",
			relPath:     `subdir\file.mkv`,
			completeDir: "",
			category:    "",
			want:        "subdir/file.mkv",
		},
		{
			name:        "relPath equals completeDir, no remainder",
			relPath:     "complete",
			completeDir: "complete",
			category:    "",
			want:        "complete",
		},
		{
			name:        "relPath equals category, no remainder",
			relPath:     "Movies",
			completeDir: "",
			category:    "Movies",
			want:        "Movies",
		},
		{
			name:        "leading slash in relPath stripped",
			relPath:     "/complete/Movies/file.mkv",
			completeDir: "complete",
			category:    "Movies",
			want:        "complete/Movies/file.mkv",
		},
		{
			name:        "completeDir with surrounding slashes is trimmed",
			relPath:     "complete/Movies/file.mkv",
			completeDir: "/complete/",
			category:    "Movies",
			want:        "complete/Movies/file.mkv",
		},
		// category and relPath are client-reachable (SABnzbd-emulation/
		// Stremio/manual-upload endpoints), and the result of this function
		// is joined directly against *cfg.Import.ImportDir by the symlink
		// and STRM writers - a "../../.." that survives here becomes a real
		// escape from ImportDir (an arbitrary-delete-then-symlink primitive
		// in the symlink writer's case). See also TestBuildLibraryRelPath_Traversal.
		{
			name:        "traversal via category clamped at root",
			relPath:     "file.mkv",
			completeDir: "",
			category:    "../../../etc",
			want:        "etc/file.mkv",
		},
		{
			name:        "traversal via relPath clamped at root",
			relPath:     "../../../etc/passwd",
			completeDir: "",
			category:    "",
			want:        "etc/passwd",
		},
		{
			name:        "traversal via completeDir clamped at root",
			relPath:     "file.mkv",
			completeDir: "../../etc",
			category:    "",
			want:        "etc/file.mkv",
		},
		{
			name:        "traversal deep enough to target root itself",
			relPath:     "../../../../../../../../etc/passwd",
			completeDir: "",
			category:    "",
			want:        "etc/passwd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildLibraryRelPath(tt.relPath, tt.completeDir, tt.category)
			if got != tt.want {
				t.Errorf("buildLibraryRelPath(%q, %q, %q) = %q; want %q",
					tt.relPath, tt.completeDir, tt.category, got, tt.want)
			}
		})
	}
}
