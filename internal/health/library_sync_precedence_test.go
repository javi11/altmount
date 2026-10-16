package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/metadata"
	metapb "github.com/kipsilabs/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Import links can remain after ARR imports a file. A sync must keep the
// official library association so subsequent repairs target the ARR library.
func TestSyncLibrary_PrefersLibraryOverImportPath(t *testing.T) {
	for _, libraryKind := range []string{"symlink", "strm", "absent"} {
		for _, importKind := range []string{"symlink", "strm"} {
			t.Run(libraryKind+"_"+importKind, func(t *testing.T) {
				repo, db := setupWorkerTestDB(t)
				_, err := db.Exec(`CREATE TABLE system_state (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
				require.NoError(t, err)
				root := t.TempDir()
				mountDir := filepath.Join(root, "mount")
				libraryDir := filepath.Join(root, "library")
				importDir := filepath.Join(root, "import")
				for _, dir := range []string{mountDir, libraryDir, importDir} {
					require.NoError(t, os.MkdirAll(dir, 0o755))
				}
				virtualPath := "movies/release/movie.mkv"
				target := filepath.Join(mountDir, filepath.FromSlash(virtualPath))
				require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
				require.NoError(t, os.WriteFile(target, []byte("movie"), 0o644))
				ms := metadata.NewMetadataService(filepath.Join(root, "metadata"))
				meta := ms.CreateFileMetadata(5, "release.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
					nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "")
				require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))

				createLink := func(dir, kind string) string {
					t.Helper()
					p := filepath.Join(dir, "movie.mkv")
					if kind == "strm" {
						p += ".strm"
						require.NoError(t, os.WriteFile(p, []byte("http://localhost/stream?path=/movies/release/movie.mkv"), 0o644))
					} else {
						require.NoError(t, os.Symlink(target, p))
					}
					return p
				}
				importPath := createLink(importDir, importKind)
				wantPath := importPath
				if libraryKind != "absent" {
					wantPath = createLink(libraryDir, libraryKind)
				}
				cfg := config.DefaultConfig()
				cleanupEnabled := libraryKind != "absent"
				cfg.Health.CleanupOrphanedMetadata = &cleanupEnabled
				cfg.Import.ImportStrategy = config.ImportStrategySYMLINK
				cfg.Import.ImportDir = &importDir
				cfg.Health.LibraryDir = &libraryDir
				cfg.Metadata.RootPath = filepath.Join(root, "metadata")
				cfg.MountPath = mountDir
				manager := config.NewManager(cfg, "")
				worker := NewLibrarySyncWorker(ms, repo, manager.GetConfig, manager, &MockRcloneClient{})
				ctx := context.Background()

				// Exercise discovery, an ARR-linked row, and healing a stale import path.
				for pass := range 3 {
					if pass == 1 {
						require.NoError(t, repo.UpdateLibraryPath(ctx, virtualPath, wantPath))
					} else if pass == 2 {
						require.NoError(t, repo.UpdateLibraryPath(ctx, virtualPath, importPath))
					}
					worker.SyncLibrary(ctx, false)
					h, err := repo.GetFileHealth(ctx, virtualPath)
					require.NoError(t, err)
					require.NotNil(t, h)
					require.NotNil(t, h.LibraryPath)
					assert.Equal(t, wantPath, *h.LibraryPath, "sync pass %d", pass)
					count, err := repo.CountHealthItems(ctx, nil, nil, "")
					require.NoError(t, err)
					assert.Equal(t, 1, count)
					assert.True(t, ms.FileExists(virtualPath))
					_, err = os.Lstat(wantPath)
					require.NoError(t, err)
					_, err = os.Lstat(importPath)
					require.NoError(t, err)
					_, err = os.Stat(target)
					require.NoError(t, err)
				}
			})
		}
	}
}

// Association precedence must not change which orphan the existing two-pass
// cleanup selects when both directories contain a STRM for missing metadata.
func TestSyncLibrary_LibraryPreferencePreservesOrphanCleanupTarget(t *testing.T) {
	repo, db := setupWorkerTestDB(t)
	_, err := db.Exec(`CREATE TABLE system_state (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	root := t.TempDir()
	libraryDir := filepath.Join(root, "library")
	importDir := filepath.Join(root, "import")
	metaDir := filepath.Join(root, "metadata")
	for _, dir := range []string{libraryDir, importDir, metaDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	libraryPath := filepath.Join(libraryDir, "movie.strm")
	importPath := filepath.Join(importDir, "movie.strm")
	for _, p := range []string{libraryPath, importPath} {
		require.NoError(t, os.WriteFile(p, []byte("http://localhost/stream?path=/movies/missing.mkv"), 0o644))
	}
	cfg := config.DefaultConfig()
	cleanupEnabled := true
	cfg.Health.CleanupOrphanedMetadata = &cleanupEnabled
	cfg.Health.LibraryDir = &libraryDir
	cfg.Import.ImportDir = &importDir
	cfg.Import.ImportStrategy = config.ImportStrategySTRM
	cfg.Metadata.RootPath = metaDir
	cfg.MountPath = filepath.Join(root, "mount")
	manager := config.NewManager(cfg, "")
	worker := NewLibrarySyncWorker(metadata.NewMetadataService(metaDir), repo, manager.GetConfig, manager, &MockRcloneClient{})
	worker.SyncLibrary(context.Background(), false)
	for _, p := range []string{libraryPath, importPath} {
		_, err := os.Lstat(p)
		require.NoError(t, err, "first pass only marks orphan candidates")
	}
	worker.SyncLibrary(context.Background(), false)
	_, err = os.Lstat(libraryPath)
	require.NoError(t, err, "official STRM must retain the existing cleanup protection")
	_, err = os.Lstat(importPath)
	require.True(t, os.IsNotExist(err), "existing cleanup selects the import STRM")
}
