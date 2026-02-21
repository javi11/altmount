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

func TestCreateSymlinks_WithCategoryInjection(t *testing.T) {
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

	// Scenario 1: Path DOES NOT include category -> Should inject
	// Full path in mount: /movies/MyMovie.mkv
	// Category: movies
	// Result: /import/movies/MyMovie.mkv
	
	fullPath1 := "/MyMovie.mkv" // Path relative to mount root
	category1 := "movies"
	
	// Create metadata file
	metaFilePath1 := filepath.Join(metadataDir, "MyMovie.mkv.meta")
	err = os.WriteFile(metaFilePath1, []byte("metadata content"), 0644)
	require.NoError(t, err)

	// Create actual file
	actualFilePath1 := filepath.Join(mountDir, "MyMovie.mkv")
	err = os.WriteFile(actualFilePath1, []byte("movie content"), 0644)
	require.NoError(t, err)

	// Scenario 2: Path ALREADY includes category -> Should NOT inject
	// Full path in mount: /tv/MyShow/Episode.mkv
	// Category: tv
	// Result: /import/tv/MyShow/Episode.mkv (no double category)

	fullPath2 := "/tv/MyShow/Episode.mkv"
	category2 := "tv"

	// Create metadata file
	metaDir2 := filepath.Join(metadataDir, "tv", "MyShow")
	err = os.MkdirAll(metaDir2, 0755)
	require.NoError(t, err)
	metaFilePath2 := filepath.Join(metaDir2, "Episode.mkv.meta")
	err = os.WriteFile(metaFilePath2, []byte("metadata content"), 0644)
	require.NoError(t, err)

	// Create actual file
	actualFileDir2 := filepath.Join(mountDir, "tv", "MyShow")
	err = os.MkdirAll(actualFileDir2, 0755)
	require.NoError(t, err)
	actualFilePath2 := filepath.Join(actualFileDir2, "Episode.mkv")
	err = os.WriteFile(actualFilePath2, []byte("show content"), 0644)
	require.NoError(t, err)

	// Setup Config
	cfg := &config.Config{
		Import: config.ImportConfig{
			ImportStrategy: config.ImportStrategySYMLINK,
			ImportDir:      &importDir,
		},
		Metadata: config.MetadataConfig{
			RootPath: metadataDir,
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

	// Test Scenario 1: Injection Needed
	item1 := &database.ImportQueueItem{
		ID:       1,
		Category: &category1,
	}
	
	err = coord.CreateSymlinks(ctx, item1, fullPath1)
	require.NoError(t, err)

	// Check generated symlink: Should be inside 'movies' folder
	expectedSymlinkPath1 := filepath.Join(importDir, "movies", "MyMovie.mkv")
	assert.FileExists(t, expectedSymlinkPath1)

	target1, err := os.Readlink(expectedSymlinkPath1)
	require.NoError(t, err)
	assert.Equal(t, actualFilePath1, target1)

	// Test Scenario 2: No Injection Needed
	item2 := &database.ImportQueueItem{
		ID:       2,
		Category: &category2,
	}
	
	err = coord.CreateSymlinks(ctx, item2, fullPath2)
	require.NoError(t, err)

	// Check generated symlink: Should be inside 'tv' folder (only once)
	expectedSymlinkPath2 := filepath.Join(importDir, "tv", "MyShow", "Episode.mkv")
	assert.FileExists(t, expectedSymlinkPath2)
	
	// Ensure double category didn't happen
	unexpectedPath2 := filepath.Join(importDir, "tv", "tv", "MyShow", "Episode.mkv")
	assert.NoFileExists(t, unexpectedPath2)

	target2, err := os.Readlink(expectedSymlinkPath2)
	require.NoError(t, err)
	assert.Equal(t, actualFilePath2, target2)
}
