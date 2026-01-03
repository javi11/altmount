package importer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_GetFailedNzbFolder(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "altmount-test")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	
	s := &Service{
		configGetter: func() *config.Config {
			return &config.Config{
				Database: config.DatabaseConfig{
					Path: dbPath,
				},
			}
		},
	}

	expectedBase := filepath.Join(tempDir, ".nzbs", "failed")

	t.Run("no category", func(t *testing.T) {
		assert.Equal(t, expectedBase, s.GetFailedNzbFolder())
	})

	t.Run("with category", func(t *testing.T) {
		assert.Equal(t, filepath.Join(expectedBase, "movies"), s.GetFailedNzbFolder("movies"))
	})

	t.Run("with category and dot-dot", func(t *testing.T) {
		assert.Equal(t, filepath.Join(expectedBase, "movies"), s.GetFailedNzbFolder("../movies"))
	})
}

func TestService_BuildCategoryPath(t *testing.T) {
	s := &Service{
		configGetter: func() *config.Config {
			return &config.Config{
				SABnzbd: config.SABnzbdConfig{
					Categories: []config.SABnzbdCategory{
						{Name: "movies", Dir: "MoviesDir"},
						{Name: "tv", Dir: ""}, // Empty dir, should fallback to Name
						{Name: config.DefaultCategoryName, Dir: "CompleteDir"},
					},
				},
			}
		},
	}

	t.Run("existing category with dir", func(t *testing.T) {
		assert.Equal(t, "MoviesDir", s.buildCategoryPath("movies"))
	})

	t.Run("existing category with empty dir", func(t *testing.T) {
		assert.Equal(t, "tv", s.buildCategoryPath("tv"))
	})

	t.Run("default category", func(t *testing.T) {
		assert.Equal(t, "CompleteDir", s.buildCategoryPath(config.DefaultCategoryName))
		assert.Equal(t, "CompleteDir", s.buildCategoryPath(""))
	})

	t.Run("non-existing category", func(t *testing.T) {
		assert.Equal(t, "other", s.buildCategoryPath("other"))
	})
}

func TestService_CalculateProcessVirtualDir(t *testing.T) {
	s := &Service{
		configGetter: func() *config.Config {
			return &config.Config{
				SABnzbd: config.SABnzbdConfig{
					CompleteDir: "/data/complete",
					Categories: []config.SABnzbdCategory{
						{Name: "movies", Dir: "movies"},
					},
				},
			}
		},
	}

	t.Run("no category", func(t *testing.T) {
		item := &database.ImportQueueItem{
			NzbPath: "/tmp/test.nzb",
		}
		basePath := ""
		virtualDir := s.calculateProcessVirtualDir(item, &basePath)
		assert.Equal(t, "/data/complete", virtualDir)
	})

	t.Run("with category not in path", func(t *testing.T) {
		cat := "movies"
		item := &database.ImportQueueItem{
			NzbPath:  "/tmp/test.nzb",
			Category: &cat,
		}
		basePath := ""
		virtualDir := s.calculateProcessVirtualDir(item, &basePath)
		assert.Equal(t, "/data/complete/movies", virtualDir)
		assert.Equal(t, "movies", basePath)
	})

	t.Run("with category already in path", func(t *testing.T) {
		cat := "movies"
		// If NzbPath is already in a folder named movies
		item := &database.ImportQueueItem{
			NzbPath:  "/data/complete/movies/test.nzb",
			Category: &cat,
		}
		basePath := "/data/complete"
		virtualDir := s.calculateProcessVirtualDir(item, &basePath)
		assert.Equal(t, "/data/complete/movies", virtualDir)
		assert.Equal(t, "/data/complete", basePath) // Should NOT append movies again
	})
}
