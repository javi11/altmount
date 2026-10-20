package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A sibling import directory can share the mount's name. Its links must never
// be classified as mount targets or rewritten when the mount location changes.
func TestSyncWalkersRespectMountBoundary(t *testing.T) {
	for _, walkerName := range []string{"library", "import"} {
		for _, migrate := range []bool{false, true} {
			name := walkerName + "/scan"
			if migrate {
				name = walkerName + "/mount_change"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				oldMount := filepath.Join(root, "altmount")
				mount := oldMount
				if migrate {
					mount = filepath.Join(root, "new_mount")
				}
				scanDir := filepath.Join(root, "library")
				require.NoError(t, os.MkdirAll(scanDir, 0o755))
				realTarget := filepath.Join(oldMount, "TV", "episode.mkv")
				siblingTarget := filepath.Join(oldMount+"_symlinks", "TV", "episode.mkv")
				realLink := filepath.Join(scanDir, "episode.mkv")
				mountLink := filepath.Join(scanDir, "mount-root")
				siblingLink := filepath.Join(scanDir, "sibling.mkv")
				require.NoError(t, os.Symlink(realTarget, realLink))
				require.NoError(t, os.Symlink(oldMount, mountLink))
				require.NoError(t, os.Symlink(siblingTarget, siblingLink))
				cfg := config.DefaultConfig()
				cfg.MountPath = mount
				cfg.Health.LibraryDir = &scanDir
				cfg.Import.ImportDir = &scanDir
				worker := &LibrarySyncWorker{configGetter: func() *config.Config { return cfg }}
				walk := worker.getAllLibraryFiles
				if walkerName == "import" {
					walk = worker.getAllImportDirFiles
				}
				oldPath, newPath := "", ""
				if migrate {
					oldPath, newPath = oldMount, mount
				}
				files, updated, walkErrors, err := walk(context.Background(), oldPath, newPath)
				require.NoError(t, err)
				assert.Zero(t, walkErrors)
				expectedUpdates := 0
				if migrate {
					expectedUpdates = 2
				}
				assert.Equal(t, expectedUpdates, updated)
				assert.Equal(t, map[string]string{filepath.Join(mount, "TV", "episode.mkv"): realLink, mount: mountLink}, files.Symlinks)
				target, err := os.Readlink(siblingLink)
				require.NoError(t, err)
				assert.Equal(t, siblingTarget, target, "sibling links must remain untouched")
				target, err = os.Readlink(realLink)
				require.NoError(t, err)
				assert.Equal(t, filepath.Join(mount, "TV", "episode.mkv"), target)
			})
		}
	}
}

// NONE imports discover regular files directly in the mount; a sibling library
// that happens to share its prefix must not be treated as that mount.
func TestLibrarySyncNoneRejectsSiblingMountPrefix(t *testing.T) {
	root := t.TempDir()
	mount := filepath.Join(root, "altmount")
	sibling := mount + "_library"
	for _, dir := range []string{mount, sibling} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "episode.mkv"), []byte("video"), 0o644))
	}
	cfg := config.DefaultConfig()
	cfg.MountPath = mount
	cfg.Import.ImportStrategy = config.ImportStrategyNone
	worker := &LibrarySyncWorker{configGetter: func() *config.Config { return cfg }}
	for _, dir := range []string{mount, sibling} {
		cfg.Health.LibraryDir = &dir
		files, updated, walkErrors, err := worker.getAllLibraryFiles(context.Background(), "", "")
		require.NoError(t, err)
		assert.Zero(t, updated)
		assert.Zero(t, walkErrors)
		if dir == mount {
			assert.Equal(t, map[string]string{filepath.Join(mount, "episode.mkv"): filepath.Join(mount, "episode.mkv")}, files.Symlinks)
		} else {
			assert.Empty(t, files.Symlinks)
		}
	}
}
