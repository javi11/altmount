package api

import (
	"testing"

	"github.com/javi11/altmount/internal/config"
)

// TestBuildCategoryPathNormalizesNames pins that a configured category resolves
// regardless of surrounding whitespace or letter case. Config validation
// rejects names that collide case-insensitively, so lookup must apply the same
// rule — and it must do so without Validate rewriting the stored config.
func TestBuildCategoryPathNormalizesNames(t *testing.T) {
	cfg := &config.Config{}
	cfg.SABnzbd.Categories = []config.SABnzbdCategory{
		{Name: "  Movies  ", Dir: "/data/movies"},
		{Name: "TV", Dir: "/data/tv"},
	}

	s := &Server{configManager: &mockConfigManager{cfg: cfg}}

	tests := []struct {
		name     string
		category string
		want     string
	}{
		{"padded config name matches exact request", "Movies", "/data/movies"},
		{"padded config name matches padded request", " Movies ", "/data/movies"},
		{"lookup is case-insensitive", "movies", "/data/movies"},
		{"unpadded name still matches", "TV", "/data/tv"},
		{"different case on unpadded name", "tv", "/data/tv"},
		{"unknown category falls back to its own name", "Books", "Books"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.buildCategoryPath(tt.category); got != tt.want {
				t.Fatalf("buildCategoryPath(%q) = %q, want %q", tt.category, got, tt.want)
			}
		})
	}
}
