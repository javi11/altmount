package nzbfilesystem

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveLibraryOutsideMount_NoMountLookups(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	mount := filepath.Join(base, "mount")
	require.NoError(t, os.Mkdir(mount, 0o755))
	alias := filepath.Join(base, "alias")
	require.NoError(t, os.Symlink(filepath.Join(mount, "missing", "directory"), alias))
	for _, root := range []string{mount, filepath.Join(mount, "child"), alias, filepath.Join(alias, "nested")} {
		t.Run(root, func(t *testing.T) {
			_, safe := resolveLibraryOutsideMount(root, mount, func(p string) (os.FileInfo, error) {
				// Even a lookup of the mount root is forbidden; a symlink target
				// must be rejected before its first component on the mount is read.
				require.NotEqual(t, mount, p, "lookup on mount root")
				require.NotContains(t, p, mount+string(filepath.Separator), "lookup inside mount")
				return os.Lstat(p)
			})
			require.False(t, safe)
		})
	}
}

func TestCanCleanLibrary_UnresolvablePathsSkipCleanup(t *testing.T) {
	base := t.TempDir()
	mount := filepath.Join(base, "mount")
	require.NoError(t, os.Mkdir(mount, 0o755))
	library := filepath.Join(base, "library")
	require.NoError(t, os.Mkdir(library, 0o755))
	cycle := filepath.Join(base, "cycle")
	require.NoError(t, os.Symlink("cycle", cycle))
	for _, tc := range []struct{ name, root, mount string }{
		{"missing library", filepath.Join(base, "missing"), mount},
		{"missing mount", library, filepath.Join(base, "missing")},
		{"library symlink cycle", cycle, mount},
		{"mount symlink cycle", library, cycle},
	} {
		t.Run(tc.name, func(t *testing.T) { require.False(t, canCleanLibrary(tc.root, tc.root, tc.mount)) })
	}
}
