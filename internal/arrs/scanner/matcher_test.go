package scanner

import (
	"testing"

	"golift.io/starr/radarr"
)

// movieWithFile builds a Radarr movie that has a file at the given path.
func movieWithFile(path string) *radarr.Movie {
	return &radarr.Movie{
		Title:   "Test Movie",
		HasFile: true,
		MovieFile: &radarr.MovieFile{
			ID:   42,
			Path: path,
		},
	}
}

func TestRadarrFileMatchesTarget(t *testing.T) {
	tests := []struct {
		name         string
		movie        *radarr.Movie
		filePath     string
		relativePath string
		want         bool
	}{
		{
			name:     "nil movie never matches",
			movie:    nil,
			filePath: "/movies/Film (2020)/Film.mkv",
			want:     false,
		},
		{
			name:     "movie without file never matches",
			movie:    &radarr.Movie{Title: "No File", HasFile: false},
			filePath: "/movies/Film (2020)/Film.mkv",
			want:     false,
		},
		{
			name: "HasFile true but nil MovieFile never matches",
			movie: &radarr.Movie{
				Title:     "Inconsistent",
				HasFile:   true,
				MovieFile: nil,
			},
			filePath: "/movies/Film (2020)/Film.mkv",
			want:     false,
		},
		{
			name:     "exact path match",
			movie:    movieWithFile("/movies/Film (2020)/Film.mkv"),
			filePath: "/movies/Film (2020)/Film.mkv",
			want:     true,
		},
		{
			name:     "basename match when parent directory differs",
			movie:    movieWithFile("/data/movies/Film (2020)/Film.mkv"),
			filePath: "/mnt/altmount/Film (2020)/Film.mkv",
			want:     true,
		},
		{
			name:     "different basename and path does not match",
			movie:    movieWithFile("/movies/Other (2019)/Other.mkv"),
			filePath: "/movies/Film (2020)/Film.mkv",
			want:     false,
		},
		{
			name:     "strm-stripped match: .strm filePath shares the movie stem",
			movie:    movieWithFile("/movies/Film (2020)/Film.mkv"),
			filePath: "/movies/Film (2020)/Film.strm",
			want:     true,
		},
		{
			name:     "strm-stripped mismatch: .strm stem differs from movie stem",
			movie:    movieWithFile("/movies/Film (2020)/Film.mkv"),
			filePath: "/movies/Film (2020)/Different.strm",
			want:     false,
		},
		{
			name:         "relative-suffix match on full relative path",
			movie:        movieWithFile("/data/movies/Film (2020)/Film.mkv"),
			filePath:     "/some/unrelated/path-xyz.mkv",
			relativePath: "Film (2020)/Film.mkv",
			want:         true,
		},
		{
			name:         "relative-suffix match with .strm-stripped relative path",
			movie:        movieWithFile("/data/movies/Film (2020)/Film.mkv"),
			filePath:     "/some/unrelated/path-xyz.mkv",
			relativePath: "Film (2020)/Film.strm",
			want:         true,
		},
		{
			name:         "relative path that is not a suffix does not match",
			movie:        movieWithFile("/data/movies/Film (2020)/Film.mkv"),
			filePath:     "/some/unrelated/path-xyz.mkv",
			relativePath: "Other (2019)/Other.mkv",
			want:         false,
		},
		{
			name:     "completely different healthy file is not condemned",
			movie:    movieWithFile("/data/movies/Healthy (2021)/Healthy.mkv"),
			filePath: "/data/movies/Corrupt (2020)/Corrupt.mkv",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := radarrFileMatchesTarget(tt.movie, tt.filePath, tt.relativePath)
			if got != tt.want {
				t.Errorf("radarrFileMatchesTarget(%v, %q, %q) = %v, want %v",
					tt.movie, tt.filePath, tt.relativePath, got, tt.want)
			}
		})
	}
}
