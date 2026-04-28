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
		itemID       int64
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
		{
			name:         "no category nzb in watch dir subdirectory",
			nzbPath:      "/config/.nzbs/Show.S01E05.nzb",
			basePath:     "Plex_Media/Series/Show (2026)/Season 01",
			expectedPath: "/mnt/remotes/altmount/Plex_Media/Series/Show (2026)/Season 01",
		},
		{
			name:         "nzb in queue_id subfolder (no basePath)",
			nzbPath:      "/config/.nzbs/tv/22/Show.S01E01.nzb.gz",
			basePath:     "",
			category:     "tv",
			itemID:       22,
			expectedPath: "/mnt/remotes/altmount/tv",
		},
		{
			name:         "nzb in queue_id subfolder with basePath",
			nzbPath:      "/config/.nzbs/tv/22/Show.S01E01.nzb.gz",
			basePath:     "media",
			category:     "tv",
			itemID:       22,
			expectedPath: "/mnt/remotes/altmount/media/tv",
		},
		{
			name:         "failed nzb in queue_id subfolder",
			nzbPath:      "/config/.nzbs/failed/tv/22/Show.S01E01.nzb.gz",
			basePath:     "",
			category:     "tv",
			itemID:       22,
			expectedPath: "/mnt/remotes/altmount/tv",
		},
		{
			name:         "nzb in queue_id subfolder no category",
			nzbPath:      "/config/.nzbs/22/Show.S01E01.nzb.gz",
			basePath:     "",
			itemID:       22,
			expectedPath: "/mnt/remotes/altmount",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &database.ImportQueueItem{
				NzbPath: filepath.FromSlash(tt.nzbPath),
				ID:      tt.itemID,
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
