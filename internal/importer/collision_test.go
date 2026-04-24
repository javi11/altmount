package importer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNextCollisionPath(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		existing []string
		want     string
	}{
		{
			name:     "compound .nzb.gz preserved",
			filename: "movie.nzb.gz",
			existing: []string{"movie.nzb.gz"},
			want:     "movie_1.nzb.gz",
		},
		{
			name:     "uppercase compound extension preserved",
			filename: "FOO.NZB.GZ",
			existing: []string{"FOO.NZB.GZ"},
			want:     "FOO_1.NZB.GZ",
		},
		{
			name:     "plain .nzb",
			filename: "bar.nzb",
			existing: []string{"bar.nzb"},
			want:     "bar_1.nzb",
		},
		{
			name:     "skips taken suffixes",
			filename: "movie.nzb.gz",
			existing: []string{"movie.nzb.gz", "movie_1.nzb.gz", "movie_2.nzb.gz"},
			want:     "movie_3.nzb.gz",
		},
		{
			name:     "no recognised extension leaves empty ext",
			filename: "weird",
			existing: []string{"weird"},
			want:     "weird_1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sub := t.TempDir()
			for _, name := range tc.existing {
				if err := os.WriteFile(filepath.Join(sub, name), []byte("x"), 0o644); err != nil {
					t.Fatalf("seed %s: %v", name, err)
				}
			}

			got := nextCollisionPath(sub, tc.filename)
			want := filepath.Join(sub, tc.want)
			if got != want {
				t.Fatalf("nextCollisionPath(%q) = %q, want %q", tc.filename, got, want)
			}
		})
	}
}
