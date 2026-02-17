package importer

import (
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestCalculateProcessVirtualDir_FailedPath(t *testing.T) {
	// Setup
	s := &Service{
		configGetter: func() *config.Config {
			return &config.Config{
				Database: config.DatabaseConfig{
					Path: "/config/altmount.db",
				},
				SABnzbd: config.SABnzbdConfig{
					CompleteDir: "/mnt/remotes/altmount",
				},
			}
		},
	}

	tests := []struct {
		name         string
		nzbPath      string
		basePath     string
		category     string
		expectedPath string
	}{
		{
			name:         "normal nzb in root",
			nzbPath:      "/config/.nzbs/Movie.nzb",
			basePath:     "movies",
			expectedPath: "/mnt/remotes/altmount/movies",
		},
		{
			name:         "failed nzb in root",
			nzbPath:      "/config/.nzbs/failed/Movie.nzb",
			basePath:     "movies",
			expectedPath: "/mnt/remotes/altmount/movies",
		},
		{
			name:         "failed nzb in category subfolder",
			nzbPath:      "/config/.nzbs/failed/tv/Show.nzb",
			basePath:     "media",
			category:     "tv",
			expectedPath: "/mnt/remotes/altmount/media/tv",
		},
		{
			name:         "normal nzb in category subfolder",
			nzbPath:      "/config/.nzbs/tv/Show.nzb",
			basePath:     "media",
			category:     "tv",
			expectedPath: "/mnt/remotes/altmount/media/tv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &database.ImportQueueItem{
				NzbPath: filepath.FromSlash(tt.nzbPath),
			}
			if tt.category != "" {
				item.Category = &tt.category
			}
			basePath := tt.basePath

			result := s.calculateProcessVirtualDir(item, &basePath)

			// Normalize separators for comparison
			result = filepath.ToSlash(result)
			expected := filepath.ToSlash(tt.expectedPath)

			assert.Equal(t, expected, result)
		})
	}
}
