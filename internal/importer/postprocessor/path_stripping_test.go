package postprocessor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSymlinks_WithStripping(t *testing.T) {
	// Setup temporary directories
	tmpDir := t.TempDir()
	metadataDir := filepath.Join(tmpDir, "metadata")
	importDir := filepath.Join(tmpDir, "import")
	mountDir := filepath.Join(tmpDir, "mount")

	err := os.MkdirAll(metadataDir, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(importDir, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(mountDir, 0755)
	require.NoError(t, err)

	ctx := context.Background()

	// Create a dummy metadata file
	// Full path in mount: /complete/tv/show/episode.mkv
	// Metadata path: /metadata/complete/tv/show/episode.mkv.meta
	fullPath := "/complete/tv/show/episode.mkv"
	metaDir := filepath.Join(metadataDir, "complete", "tv", "show")
	err = os.MkdirAll(metaDir, 0755)
	require.NoError(t, err)

	metaFilePath := filepath.Join(metaDir, "episode.mkv.meta")
	err = os.WriteFile(metaFilePath, []byte("metadata content"), 0644)
	require.NoError(t, err)

	// Create the actual file in mount
	actualFileDir := filepath.Join(mountDir, "complete", "tv", "show")
	err = os.MkdirAll(actualFileDir, 0755)
	require.NoError(t, err)
	actualFilePath := filepath.Join(actualFileDir, "episode.mkv")
	err = os.WriteFile(actualFilePath, []byte("movie content"), 0644)
	require.NoError(t, err)

	// Setup Config with stripping
	cfg := &config.Config{
		Import: config.ImportConfig{
			ImportStrategy: config.ImportStrategySYMLINK,
			ImportDir:      &importDir,
		},
		Metadata: config.MetadataConfig{
			RootPath: metadataDir,
		},
		SABnzbd: config.SABnzbdConfig{
			CompleteDir: "/complete",
		},
		MountPath: mountDir,
	}

	configGetter := func() *config.Config {
		return cfg
	}

	// Setup Coordinator
	coord := NewCoordinator(Config{
		ConfigGetter: configGetter,
	})

	// Call CreateSymlinks
	item := &database.ImportQueueItem{ID: 1}
	
	err = coord.CreateSymlinks(ctx, item, fullPath)
	require.NoError(t, err)

	// Check generated symlink
	// It should be at /import/tv/show/episode.mkv (stripped)
	// It should point to /mount/complete/tv/show/episode.mkv (original)
	expectedSymlinkPath := filepath.Join(importDir, "tv", "show", "episode.mkv")
	assert.FileExists(t, expectedSymlinkPath)

	target, err := os.Readlink(expectedSymlinkPath)
	require.NoError(t, err)
	assert.Equal(t, actualFilePath, target)
}

func TestCreateStrmFiles_WithStripping(t *testing.T) {
	// Setup temporary directories
	tmpDir := t.TempDir()
	metadataDir := filepath.Join(tmpDir, "metadata")
	importDir := filepath.Join(tmpDir, "import")

	err := os.MkdirAll(metadataDir, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(importDir, 0755)
	require.NoError(t, err)

	// Setup Database and User
	userRepo := setupTestDB(t)
	ctx := context.Background()

	apiKey := "test-api-key"
	adminUser := &database.User{
		UserID:  "admin",
		APIKey:  &apiKey,
		IsAdmin: true,
	}
	err = userRepo.CreateUser(ctx, adminUser)
	require.NoError(t, err)

	// Create a dummy metadata file
	// Full path in mount: /complete/movies/test.mkv
	// Metadata path: /metadata/complete/movies/test.mkv.meta
	fullPath := "/complete/movies/test.mkv"
	metaDir := filepath.Join(metadataDir, "complete", "movies")
	err = os.MkdirAll(metaDir, 0755)
	require.NoError(t, err)

	metaFilePath := filepath.Join(metaDir, "test.mkv.meta")
	err = os.WriteFile(metaFilePath, []byte("metadata content"), 0644)
	require.NoError(t, err)

	// Setup Config with stripping
	cfg := &config.Config{
		Import: config.ImportConfig{
			ImportStrategy: config.ImportStrategySTRM,
			ImportDir:      &importDir,
		},
		Metadata: config.MetadataConfig{
			RootPath: metadataDir,
		},
		SABnzbd: config.SABnzbdConfig{
			CompleteDir: "/complete",
		},
		WebDAV: config.WebDAVConfig{
			Port: 8080,
			Host: "localhost",
		},
	}

	configGetter := func() *config.Config {
		return cfg
	}

	// Setup Coordinator
	coord := NewCoordinator(Config{
		ConfigGetter: configGetter,
		UserRepo:     userRepo,
	})

	// Call CreateStrmFiles
	item := &database.ImportQueueItem{ID: 1}

	err = coord.CreateStrmFiles(ctx, item, fullPath)
	require.NoError(t, err)

	// Check generated STRM file
	// It should be at /import/movies/test.mkv.strm (stripped)
	// Inside it should point to /complete/movies/test.mkv (original)
	expectedStrmPath := filepath.Join(importDir, "movies", "test.mkv.strm")
	assert.FileExists(t, expectedStrmPath)

	content, err := os.ReadFile(expectedStrmPath)
	require.NoError(t, err)
	url := string(content)
	
	// Check that the URL contains the ORIGINAL path (with /complete)
	assert.Contains(t, url, "path=/complete/movies/test.mkv")
	assert.NotContains(t, url, "path=/movies/test.mkv")
}
