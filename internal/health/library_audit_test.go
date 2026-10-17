package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/database"
	"github.com/kipsilabs/altmount/internal/metadata"
	metapb "github.com/kipsilabs/altmount/internal/metadata/proto"
)

type auditEnv struct {
	worker    *LibrarySyncWorker
	meta      *metadata.MetadataService
	health    *database.HealthRepository
	libDir    string
	mountDir  string
	importDir string
}

// newAuditEnv mirrors a real install: library symlinks into the mount, either
// directly or through the import directory, which sits next to the mount
// with a name sharing its prefix (/x/altmount and /x/altmount_symlinks).
func newAuditEnv(t *testing.T, withImportDir bool) *auditEnv {
	t.Helper()
	root := t.TempDir()
	env := &auditEnv{
		libDir:   filepath.Join(root, "media"),
		mountDir: filepath.Join(root, "altmount"),
	}
	require.NoError(t, os.MkdirAll(env.libDir, 0o755))
	metaRoot := filepath.Join(root, "metadata")
	env.meta = metadata.NewMetadataService(metaRoot)

	cfg := config.DefaultConfig()
	cfg.Metadata.RootPath = metaRoot
	cfg.MountPath = env.mountDir
	cfg.Health.LibraryDir = &env.libDir
	if withImportDir {
		env.importDir = env.mountDir + "_symlinks"
		require.NoError(t, os.MkdirAll(env.importDir, 0o755))
		cfg.Import.ImportDir = &env.importDir
	}
	mgr := config.NewManager(cfg, "")
	env.health = newAuditHealthRepo(t)
	env.worker = NewLibrarySyncWorker(env.meta, env.health, mgr.GetConfig, mgr, &MockRcloneClient{})
	return env
}

func newAuditHealthRepo(t *testing.T) *database.HealthRepository {
	t.Helper()
	db, err := database.NewDB(database.Config{Type: "sqlite", DatabasePath: filepath.Join(t.TempDir(), "audit.db")})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return database.NewHealthRepository(db.Connection(), db.Dialect())
}

// addFile writes AltMount metadata for a virtual file imported `age` ago.
func (e *auditEnv) addFile(t *testing.T, rel string, age time.Duration) {
	t.Helper()
	fm := e.meta.CreateFileMetadata(1234, "x.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY, nil,
		metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "")
	fm.CreatedAt = time.Now().Add(-age).Unix()
	require.NoError(t, e.meta.WriteFileMetadata(rel, fm))
}

// link creates a symlink at path (relative to the library unless absolute).
func (e *auditEnv) link(t *testing.T, path, target string) string {
	t.Helper()
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.libDir, path)
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.Symlink(target, path))
	return path
}

func paths(items []LibraryAuditItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Path
	}
	return out
}

const old = 30 * 24 * time.Hour

func TestLibraryAuditFindsOrphansAndDeadSymlinks(t *testing.T) {
	env := newAuditEnv(t, true)
	env.addFile(t, "TV/Show/a.mkv", old)            // linked directly
	env.addFile(t, "TV/Show/b.mkv", old)            // linked through the import dir
	env.addFile(t, "TV/Pack/S01/e1.mkv", old)       // covered by a season-folder link
	env.addFile(t, "TV/Pack/S01/e2.mkv", old)       // covered by a season-folder link
	env.addFile(t, "Movies/Old/old.mkv", old)       // orphan
	env.addFile(t, "Movies/New/new.mkv", time.Hour) // orphan, but too recent

	env.link(t, "TV/Show/a.mkv", filepath.Join(env.mountDir, "TV/Show/a.mkv"))
	hop := env.link(t, filepath.Join(env.importDir, "TV/Show/b.mkv"), filepath.Join(env.mountDir, "TV/Show/b.mkv"))
	env.link(t, "TV/Show/b.mkv", hop)
	env.link(t, "TV/Pack/Season 01", filepath.Join(env.mountDir, "TV/Pack/S01"))
	dead1 := env.link(t, "Movies/Gone/gone.mkv", filepath.Join(env.mountDir, "Movies/Gone/gone.mkv"))
	dead2 := env.link(t, "Movies/Gone2/gone2.mkv", filepath.Join(env.importDir, "Movies/Gone2/gone2.mkv"))

	// Not AltMount's: a link to a real file elsewhere is ignored.
	outside := filepath.Join(t.TempDir(), "real.mkv")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o644))
	env.link(t, "Other/real.mkv", outside)

	audit, err := env.worker.AuditLibrary(context.Background(), DefaultAuditGracePeriod)
	require.NoError(t, err)

	assert.Equal(t, 6, audit.FilesScanned)
	assert.Equal(t, 5, audit.SymlinksScanned, "only links into AltMount are counted")
	assert.Equal(t, []string{"Movies/Old/old.mkv"}, paths(audit.Orphans))
	assert.Equal(t, int64(1234), audit.Orphans[0].Size)
	assert.Equal(t, 1, audit.SkippedRecent, "a just-imported file awaiting its ARR link is not an orphan")
	assert.Equal(t, []string{dead1, dead2}, paths(audit.DeadSymlinks))
	for _, d := range audit.DeadSymlinks {
		assert.Equal(t, AuditLocationLibrary, d.Location)
	}
	assert.Empty(t, audit.OrphanCleanupBlocked)
	assert.Empty(t, audit.WalkErrors)
}

func TestLibraryAuditCleanupActsOnlyOnCurrentFindings(t *testing.T) {
	env := newAuditEnv(t, false)
	env.addFile(t, "Movies/Kept/kept.mkv", old)
	env.addFile(t, "Movies/Orphan/orphan.mkv", old)
	env.link(t, "Movies/kept.mkv", filepath.Join(env.mountDir, "Movies/Kept/kept.mkv"))
	dead := env.link(t, "Movies/dead.mkv", filepath.Join(env.mountDir, "Movies/Dead/dead.mkv"))
	kept := filepath.Join(env.libDir, "Movies/kept.mkv")
	ctx := context.Background()
	require.NoError(t, env.health.UpdateFileHealth(ctx, "Movies/Orphan/orphan.mkv", database.HealthStatusHealthy, nil, nil, nil, false))
	require.NoError(t, env.health.UpdateFileHealth(ctx, "Movies/Kept/kept.mkv", database.HealthStatusHealthy, nil, nil, nil, false))

	res, err := env.worker.CleanupLibraryAudit(context.Background(),
		[]string{"Movies/Orphan/orphan.mkv", "Movies/Kept/kept.mkv"},
		[]string{dead, kept},
		DefaultAuditGracePeriod)
	require.NoError(t, err)

	assert.Equal(t, 1, res.OrphansDeleted)
	assert.Equal(t, 1, res.DeadSymlinksRemoved)
	assert.ElementsMatch(t, []LibraryAuditSkip{
		{Path: "Movies/Kept/kept.mkv", Reason: "no longer orphaned"},
		{Path: kept, Reason: "no longer a dead symlink"},
	}, res.Skipped)

	assert.False(t, env.meta.FileExists("Movies/Orphan/orphan.mkv"), "orphan metadata deleted")
	assert.True(t, env.meta.FileExists("Movies/Kept/kept.mkv"), "linked file untouched")
	if rec, err := env.health.GetFileHealth(ctx, "Movies/Orphan/orphan.mkv"); err == nil {
		assert.Nil(t, rec, "orphan's health record deleted")
	}
	rec, err := env.health.GetFileHealth(ctx, "Movies/Kept/kept.mkv")
	require.NoError(t, err)
	assert.NotNil(t, rec, "linked file's health record kept")
	_, err = os.Lstat(dead)
	assert.True(t, os.IsNotExist(err), "dead symlink removed")
	_, err = os.Lstat(kept)
	assert.NoError(t, err, "live symlink untouched")

	require.NotNil(t, res.Audit)
	assert.Empty(t, res.Audit.Orphans)
	assert.Empty(t, res.Audit.DeadSymlinks)
}

// An empty or unmounted library makes every file look orphaned. The audit must
// say so and refuse to delete anything.
func TestLibraryAuditBlocksOrphanCleanupWhenLibraryLooksUnmounted(t *testing.T) {
	env := newAuditEnv(t, false)
	env.addFile(t, "Movies/A/a.mkv", old)
	env.addFile(t, "Movies/B/b.mkv", old)

	audit, err := env.worker.AuditLibrary(context.Background(), DefaultAuditGracePeriod)
	require.NoError(t, err)
	assert.Len(t, audit.Orphans, 2)
	assert.NotEmpty(t, audit.OrphanCleanupBlocked)
	assert.NotEmpty(t, audit.Warning)

	res, err := env.worker.CleanupLibraryAudit(context.Background(), []string{"Movies/A/a.mkv"}, nil, DefaultAuditGracePeriod)
	require.NoError(t, err)
	assert.Zero(t, res.OrphansDeleted)
	require.Len(t, res.Skipped, 1)
	assert.True(t, env.meta.FileExists("Movies/A/a.mkv"), "nothing deleted while the library looks unmounted")
}

func TestLibraryAuditRequiresLibraryDir(t *testing.T) {
	env := newAuditEnv(t, false)
	cfg := config.DefaultConfig()
	cfg.MountPath = env.mountDir
	mgr := config.NewManager(cfg, "")
	worker := NewLibrarySyncWorker(env.meta, env.health, mgr.GetConfig, mgr, &MockRcloneClient{})
	_, err := worker.AuditLibrary(context.Background(), DefaultAuditGracePeriod)
	assert.Error(t, err)
}

// A symlink loop must end the resolution rather than hang the scan.
func TestResolveAuditLinkStopsOnLoops(t *testing.T) {
	dir := t.TempDir()
	mount := filepath.Join(dir, "altmount")
	imp := filepath.Join(dir, "altmount_symlinks")
	require.NoError(t, os.MkdirAll(imp, 0o755))
	a, b := filepath.Join(imp, "a"), filepath.Join(imp, "b")
	require.NoError(t, os.Symlink(b, a))
	require.NoError(t, os.Symlink(a, b))
	lib := filepath.Join(dir, "lib.mkv")
	require.NoError(t, os.Symlink(a, lib))

	rel, _, dead, ours := resolveAuditLink(lib, mount, imp)
	assert.Empty(t, rel)
	assert.True(t, dead)
	assert.True(t, ours)
}

func TestWithinRejectsPrefixSiblings(t *testing.T) {
	assert.True(t, within("/mnt/altmount/TV/a.mkv", "/mnt/altmount"))
	assert.False(t, within("/mnt/altmount_symlinks/TV/a.mkv", "/mnt/altmount"))
	assert.False(t, within("/mnt/altmount", "/mnt/altmount"))
	assert.False(t, within("/anything", ""))
}

// AltMount's import directory collects a symlink per import for the ARRs to
// pick up. When the file is later deleted the link stays behind, and nothing
// else removes it. The audit lists those dead links, and cleanup removes them
// along with the release folder they leave empty, but never the import
// directory's own layout (complete/ and its category folders).
func TestLibraryAuditCleansDeadImportDirSymlinks(t *testing.T) {
	env := newAuditEnv(t, true)
	env.addFile(t, "complete/TV/Live/live.mkv", old)
	env.link(t, "TV/live.mkv", filepath.Join(env.mountDir, "complete/TV/Live/live.mkv")) // keeps it from being an orphan

	live := env.link(t, filepath.Join(env.importDir, "complete/TV/Live/live.mkv"), filepath.Join(env.mountDir, "complete/TV/Live/live.mkv"))
	dead := env.link(t, filepath.Join(env.importDir, "complete/Prowl/Gone.Release/gone.mkv"), filepath.Join(env.mountDir, "complete/Prowl/Gone.Release/gone.mkv"))
	emptyCategory := filepath.Join(env.importDir, "complete/Movies")
	require.NoError(t, os.MkdirAll(emptyCategory, 0o755))

	audit, err := env.worker.AuditLibrary(context.Background(), DefaultAuditGracePeriod)
	require.NoError(t, err)
	assert.Equal(t, 2, audit.ImportSymlinksScanned)
	assert.Equal(t, 1, audit.SymlinksScanned, "import links are not library links")
	require.Len(t, audit.DeadSymlinks, 1)
	assert.Equal(t, dead, audit.DeadSymlinks[0].Path)
	assert.Equal(t, AuditLocationImport, audit.DeadSymlinks[0].Location)
	assert.Empty(t, audit.Orphans, "an import-dir link does not make a file orphaned or referenced")

	res, err := env.worker.CleanupLibraryAudit(context.Background(), nil, []string{dead}, DefaultAuditGracePeriod)
	require.NoError(t, err)
	assert.Equal(t, 1, res.DeadSymlinksRemoved)
	assert.Empty(t, res.Skipped)

	_, err = os.Lstat(dead)
	assert.True(t, os.IsNotExist(err), "dead import link removed")
	_, err = os.Stat(filepath.Join(env.importDir, "complete/Prowl/Gone.Release"))
	assert.True(t, os.IsNotExist(err), "emptied release folder pruned")
	for _, keep := range []string{filepath.Join(env.importDir, "complete/Prowl"), emptyCategory, filepath.Join(env.importDir, "complete")} {
		_, err = os.Stat(keep)
		assert.NoError(t, err, "import layout kept: %s", keep)
	}
	_, err = os.Lstat(live)
	assert.NoError(t, err, "live import link untouched")
}

// Pruning never climbs out of the import directory or below its kept depth,
// and leaves any folder that still has content.
func TestPruneEmptyImportDirs(t *testing.T) {
	imp := filepath.Join(t.TempDir(), "altmount_symlinks")
	deep := filepath.Join(imp, "complete/Cat/Release/Sub")
	require.NoError(t, os.MkdirAll(deep, 0o755))
	keepFile := filepath.Join(imp, "complete/Cat/other.txt")
	require.NoError(t, os.WriteFile(keepFile, []byte("x"), 0o644))

	pruneEmptyImportDirs(deep, imp)

	_, err := os.Stat(filepath.Join(imp, "complete/Cat/Release"))
	assert.True(t, os.IsNotExist(err), "empty release tree removed")
	_, err = os.Stat(filepath.Join(imp, "complete/Cat"))
	assert.NoError(t, err, "category folder kept")
	_, err = os.Stat(imp)
	assert.NoError(t, err, "import root kept")
}
