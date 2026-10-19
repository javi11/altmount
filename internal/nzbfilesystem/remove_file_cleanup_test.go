package nzbfilesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveFile_PhysicalDirectoryCleanup(t *testing.T) {
	for _, name := range []string{"mount root", "mount child", "unset library", "empty library", "symlink to mount", "symlink into mount", "chained symlink", "mount parent alias", "mount root alias", "library child symlink into mount", "physical path on mount", "physical path outside library", "outside library", "prefix sibling", "outside symlink", "unset mount"} {
		t.Run(name, func(t *testing.T) {
			repo, db, ms := setupStreamHealthEnv(t)
			base := t.TempDir()
			mount := filepath.Join(base, "mount")
			require.NoError(t, os.MkdirAll(mount, 0o755))
			library := mount
			keep := true
			cfg := config.DefaultConfig()
			cfg.MountPath = mount
			switch name {
			case "mount child":
				library = filepath.Join(mount, "library")
			case "symlink to mount", "symlink into mount", "chained symlink":
				target := mount
				if name == "symlink into mount" {
					target = filepath.Join(mount, "library")
					require.NoError(t, os.MkdirAll(target, 0o755))
				}
				library = filepath.Join(base, "library-link")
				require.NoError(t, os.Symlink(target, library))
				if name == "chained symlink" {
					next := filepath.Join(base, "second-link")
					require.NoError(t, os.Symlink("library-link", next))
					library = next
				}
			case "mount root alias":
				alias := filepath.Join(base, "mount-link")
				require.NoError(t, os.Symlink(mount, alias))
				cfg.MountPath = alias
			case "mount parent alias":
				alias := filepath.Join(base, "parent-link")
				require.NoError(t, os.Symlink(base, alias))
				cfg.MountPath = filepath.Join(alias, "mount")
			case "library child symlink into mount", "physical path on mount", "physical path outside library":
				library = filepath.Join(base, "library")
				require.NoError(t, os.MkdirAll(library, 0o755))
				if name == "library child symlink into mount" {
					require.NoError(t, os.Symlink(mount, filepath.Join(library, "series")))
				}
			case "outside library", "outside symlink", "unset mount":
				library = filepath.Join(base, "library")
				require.NoError(t, os.MkdirAll(library, 0o755))
				keep = false
				if name == "outside symlink" {
					alias := filepath.Join(base, "library-link")
					require.NoError(t, os.Symlink(library, alias))
					library = alias
				}
				if name == "unset mount" {
					cfg.MountPath = ""
				}
			case "prefix sibling":
				library = mount + "-library"
				keep = false
			}
			cfg.Health.LibraryDir = &library
			if name == "unset library" {
				cfg.Health.LibraryDir = nil
			}
			if name == "empty library" {
				empty := ""
				cfg.Health.LibraryDir = &empty
			}
			physicalDir := filepath.Join(library, "series", "season")
			if name == "physical path on mount" {
				physicalDir = filepath.Join(mount, "series", "season")
			}
			if name == "physical path outside library" {
				physicalDir = filepath.Join(base, "elsewhere", "season")
			}
			require.NoError(t, os.MkdirAll(physicalDir, 0o755))
			filePath := "series/season/episode.mkv"
			writeStreamMeta(t, ms, filePath)
			_, err := db.Exec(`INSERT INTO file_health (file_path, library_path, status) VALUES (?, ?, 'healthy')`, filePath, filepath.Join(physicalDir, "episode.mkv"))
			require.NoError(t, err)
			mrf := &MetadataRemoteFile{metadataService: ms, healthRepository: repo, configGetter: func() *config.Config { return cfg }}
			ok, err := mrf.RemoveFile(context.Background(), filePath)
			require.NoError(t, err)
			require.True(t, ok)
			assert.False(t, ms.FileExists(filePath), "virtual file metadata must be deleted")
			assert.False(t, ms.DirectoryExists("series/season"), "empty virtual directory metadata must be deleted")
			if keep {
				assert.DirExists(t, physicalDir, "cleanup must not traverse the mount")
			} else {
				assert.NoDirExists(t, physicalDir, "external library cleanup must be preserved")
			}
			info, err := os.Stat(library)
			require.NoError(t, err)
			assert.True(t, info.IsDir(), "cleanup must preserve its root")
		})
	}
}
