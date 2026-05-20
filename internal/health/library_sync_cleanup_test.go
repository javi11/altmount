package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveEmptyDirectories_PreservesCategoryDirs reproduces Bug 2:
// transiently-empty category folders under ImportDir get deleted by the
// library sync, which makes Radarr's RemotePathMappingCheck go red until
// the next import lands. The fix must preserve configured category dirs.
func TestRemoveEmptyDirectories_PreservesCategoryDirs(t *testing.T) {
	tests := []struct {
		name        string
		completeDir string
		categories  []config.SABnzbdCategory
		// dirs to pre-create relative to ImportDir
		setupDirs []string
		// dirs that MUST still exist after cleanup, relative to ImportDir
		mustKeep []string
		// dirs that MUST be removed (genuine release leftovers), relative to ImportDir
		mustRemove []string
	}{
		{
			name:        "no complete_dir, explicit categories — names",
			completeDir: "",
			categories: []config.SABnzbdCategory{
				{Name: "movies"},
				{Name: "tv"},
			},
			setupDirs:  []string{"movies", "tv", "movies/Some.Release.2024"},
			mustKeep:   []string{"movies", "tv"},
			mustRemove: []string{"movies/Some.Release.2024"},
		},
		{
			name:        "no complete_dir, category Dir overrides Name",
			completeDir: "",
			categories: []config.SABnzbdCategory{
				{Name: "movies", Dir: "films"},
			},
			setupDirs:  []string{"films", "films/Old.Release"},
			mustKeep:   []string{"films"},
			mustRemove: []string{"films/Old.Release"},
		},
		{
			name:        "complete_dir set + categories",
			completeDir: "complete",
			categories: []config.SABnzbdCategory{
				{Name: "movies"},
				{Name: "tv"},
			},
			setupDirs:  []string{"complete", "complete/movies", "complete/tv", "complete/movies/Stale.Release"},
			mustKeep:   []string{"complete", "complete/movies", "complete/tv"},
			mustRemove: []string{"complete/movies/Stale.Release"},
		},
		{
			name:        "no categories configured — default category dir is protected",
			completeDir: "",
			categories:  nil,
			setupDirs:   []string{config.DefaultCategoryDir, filepath.Join(config.DefaultCategoryDir, "Old.Release")},
			mustKeep:    []string{config.DefaultCategoryDir},
			mustRemove:  []string{filepath.Join(config.DefaultCategoryDir, "Old.Release")},
		},
		{
			name:        "nested category Dir (e.g. media/movies)",
			completeDir: "",
			categories: []config.SABnzbdCategory{
				{Name: "movies", Dir: "media/movies"},
			},
			setupDirs:  []string{"media", "media/movies", "media/movies/Release.X"},
			mustKeep:   []string{"media", "media/movies"},
			mustRemove: []string{"media/movies/Release.X"},
		},
		{
			// Matches the common real-world config where complete_dir is set
			// to the literal "/" — Go's filepath.Join normalizes this to the
			// same path layout as an empty complete_dir.
			name:        "complete_dir set to root slash collapses to ImportDir",
			completeDir: "/",
			categories: []config.SABnzbdCategory{
				{Name: "movies"},
				{Name: "tv"},
			},
			setupDirs:  []string{"movies", "tv", "movies/Stale.Release"},
			mustKeep:   []string{"movies", "tv"},
			mustRemove: []string{"movies/Stale.Release"},
		},
		{
			// Real-world TV layout: Show/Season N/Episode N folders. Every
			// level *below* the category must still get pruned when empty,
			// even multiple levels deep.
			name:        "deep nesting under category gets fully pruned",
			completeDir: "",
			categories: []config.SABnzbdCategory{
				{Name: "tv"},
			},
			setupDirs: []string{
				"tv",
				"tv/Walking.Dead",
				"tv/Walking.Dead/Season.02",
				"tv/Walking.Dead/Season.02/episode.x",
			},
			mustKeep: []string{"tv"},
			mustRemove: []string{
				"tv/Walking.Dead",
				"tv/Walking.Dead/Season.02",
				"tv/Walking.Dead/Season.02/episode.x",
			},
		},
		{
			// Category that contains only files (no release subdir) goes
			// empty after Sonarr imports the single .mkv. Category must
			// survive even though it is the only thing left at that level.
			name:        "category becomes directly empty after import",
			completeDir: "",
			categories: []config.SABnzbdCategory{
				{Name: "tv"},
			},
			setupDirs:  []string{"tv"},
			mustKeep:   []string{"tv"},
			mustRemove: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			importDir := t.TempDir()

			for _, rel := range tc.setupDirs {
				require.NoError(t, os.MkdirAll(filepath.Join(importDir, rel), 0o755))
			}

			cfg := &config.Config{
				MountPath: t.TempDir(), // separate, unused here
				Import: config.ImportConfig{
					ImportDir:      &importDir,
					ImportStrategy: config.ImportStrategySYMLINK,
				},
				SABnzbd: config.SABnzbdConfig{
					CompleteDir: tc.completeDir,
					Categories:  tc.categories,
				},
			}

			worker := &LibrarySyncWorker{
				configGetter: func() *config.Config { return cfg },
			}

			_, err := worker.removeEmptyDirectories(context.Background())
			require.NoError(t, err)

			for _, rel := range tc.mustKeep {
				full := filepath.Join(importDir, rel)
				_, statErr := os.Stat(full)
				assert.NoError(t, statErr, "category dir must be preserved: %s", rel)
			}
			for _, rel := range tc.mustRemove {
				full := filepath.Join(importDir, rel)
				_, statErr := os.Stat(full)
				assert.True(t, os.IsNotExist(statErr),
					"non-category empty dir must still be cleaned up: %s (stat err=%v)", rel, statErr)
			}

			// ImportDir root itself must always survive.
			_, statErr := os.Stat(importDir)
			assert.NoError(t, statErr, "ImportDir root must always survive")
		})
	}
}

// TestBuildProtectedImportDirs_NoImportDir confirms the helper degrades to
// an empty set when ImportDir is unset — there is nothing under ImportDir
// to protect because removeEmptyDirectories will not scan it either.
func TestBuildProtectedImportDirs_NoImportDir(t *testing.T) {
	t.Run("ImportDir nil", func(t *testing.T) {
		cfg := &config.Config{
			SABnzbd: config.SABnzbdConfig{
				CompleteDir: "complete",
				Categories:  []config.SABnzbdCategory{{Name: "movies"}},
			},
		}
		got := buildProtectedImportDirs(cfg)
		assert.Empty(t, got, "no ImportDir → no protected paths")
	})

	t.Run("ImportDir empty string", func(t *testing.T) {
		empty := ""
		cfg := &config.Config{
			Import:  config.ImportConfig{ImportDir: &empty},
			SABnzbd: config.SABnzbdConfig{Categories: []config.SABnzbdCategory{{Name: "tv"}}},
		}
		got := buildProtectedImportDirs(cfg)
		assert.Empty(t, got, "empty ImportDir → no protected paths")
	})

	t.Run("nil cfg", func(t *testing.T) {
		got := buildProtectedImportDirs(nil)
		assert.Empty(t, got, "nil cfg → no protected paths")
	})
}
