package health

import (
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestBuildExcludedCategoryPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected []string
	}{
		{
			name:     "nil config",
			cfg:      nil,
			expected: nil,
		},
		{
			name: "no excluded categories",
			cfg: &config.Config{
				Health: config.HealthConfig{ExcludedCategories: nil},
			},
			expected: nil,
		},
		{
			name: "explicit dir with complete dir",
			cfg: &config.Config{
				Health: config.HealthConfig{ExcludedCategories: []string{"tv"}},
				SABnzbd: config.SABnzbdConfig{
					CompleteDir: "complete",
					Categories:  []config.SABnzbdCategory{{Name: "tv", Dir: "series"}},
				},
			},
			expected: []string{"complete/series"},
		},
		{
			name: "category without dir falls back to name",
			cfg: &config.Config{
				Health: config.HealthConfig{ExcludedCategories: []string{"tv"}},
				SABnzbd: config.SABnzbdConfig{
					CompleteDir: "complete",
					Categories:  []config.SABnzbdCategory{{Name: "tv"}},
				},
			},
			expected: []string{"complete/tv"},
		},
		{
			name: "default category falls back to default dir",
			cfg: &config.Config{
				Health: config.HealthConfig{ExcludedCategories: []string{config.DefaultCategoryName}},
				SABnzbd: config.SABnzbdConfig{
					CompleteDir: "complete",
					Categories:  []config.SABnzbdCategory{{Name: config.DefaultCategoryName}},
				},
			},
			expected: []string{"complete/" + config.DefaultCategoryDir},
		},
		{
			name: "empty complete dir yields bare category dir",
			cfg: &config.Config{
				Health: config.HealthConfig{ExcludedCategories: []string{"movies"}},
				SABnzbd: config.SABnzbdConfig{
					CompleteDir: "",
					Categories:  []config.SABnzbdCategory{{Name: "movies", Dir: "movies"}},
				},
			},
			expected: []string{"movies"},
		},
		{
			name: "no categories configured, non-default name uses itself",
			cfg: &config.Config{
				Health:  config.HealthConfig{ExcludedCategories: []string{"tv"}},
				SABnzbd: config.SABnzbdConfig{CompleteDir: "complete"},
			},
			expected: []string{"complete/tv"},
		},
		{
			name: "prefixes are lower-cased",
			cfg: &config.Config{
				Health: config.HealthConfig{ExcludedCategories: []string{"TV"}},
				SABnzbd: config.SABnzbdConfig{
					CompleteDir: "Complete",
					Categories:  []config.SABnzbdCategory{{Name: "TV", Dir: "Series"}},
				},
			},
			expected: []string{"complete/series"},
		},
		{
			name: "empty and unknown names are skipped/passed through",
			cfg: &config.Config{
				Health: config.HealthConfig{ExcludedCategories: []string{"", "unknown"}},
				SABnzbd: config.SABnzbdConfig{
					CompleteDir: "complete",
					Categories:  []config.SABnzbdCategory{{Name: "tv", Dir: "series"}},
				},
			},
			// "" skipped; "unknown" has no configured category so it maps to itself.
			expected: []string{"complete/unknown"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildExcludedCategoryPrefixes(tt.cfg)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestPathHasExcludedPrefix(t *testing.T) {
	prefixes := []string{"complete/tv", "complete/movies"}

	tests := []struct {
		name     string
		path     string
		prefixes []string
		expected bool
	}{
		{"nil prefixes", "complete/tv/show/file.mkv", nil, false},
		{"exact match", "complete/tv", prefixes, true},
		{"nested match", "complete/tv/ShowName/S01E01.mkv", prefixes, true},
		{"second prefix match", "complete/movies/Film/file.mkv", prefixes, true},
		{"case-insensitive path", "Complete/TV/ShowName/file.mkv", prefixes, true},
		{"leading slash tolerated", "/complete/tv/show/file.mkv", prefixes, true},
		{"segment boundary: tv-extras not matched", "complete/tv-extras/file.mkv", prefixes, false},
		{"different category not matched", "complete/music/file.mkv", prefixes, false},
		{"prefix as substring only not matched", "complete/tvshows/file.mkv", prefixes, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, pathHasExcludedPrefix(tt.path, tt.prefixes))
		})
	}
}
