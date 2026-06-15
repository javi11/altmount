package postprocessor

import (
	"context"
	"database/sql"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSchedulerTest builds a Coordinator with a real MetadataService (temp dir)
// and a real HealthRepository (in-memory sqlite) so ScheduleHealthCheck can be
// exercised end to end. Optional mutators adjust the config before the manager
// is built (e.g. to set an import strategy).
func setupSchedulerTest(t *testing.T, mutate ...func(*config.Config)) (*Coordinator, *metadata.MetadataService, *database.HealthRepository, *sql.DB) {
	t.Helper()

	db, err := sql.Open("sqlite3", "file::memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE file_health (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT NOT NULL UNIQUE,
			library_path TEXT,
			status TEXT NOT NULL,
			last_checked DATETIME,
			last_error TEXT,
			retry_count INTEGER DEFAULT 0,
			max_retries INTEGER DEFAULT 3,
			repair_retry_count INTEGER DEFAULT 0,
			max_repair_retries INTEGER DEFAULT 3,
			source_nzb_path TEXT,
			error_details TEXT,
			metadata TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			release_date DATETIME,
			scheduled_check_at DATETIME,
			priority INTEGER DEFAULT 0,
			streaming_failure_count INTEGER DEFAULT 0,
			is_masked BOOLEAN DEFAULT FALSE,
			indexer TEXT DEFAULT NULL
		);
	`)
	require.NoError(t, err)

	repo := database.NewHealthRepository(db, database.DialectSQLite)
	ms := metadata.NewMetadataService(t.TempDir())

	cfg := config.DefaultConfig()
	// Health checking is disabled by default; enable it so the scheduling path runs.
	// (Tests that exercise the disabled guard override this via a mutator below.)
	healthEnabled := true
	cfg.Health.Enabled = &healthEnabled
	// Directory repair resolution is opt-in (defaults to false); enable it so the
	// stale-repair cleanup path is exercised.
	resolveRepairs := true
	cfg.Health.ResolveRepairOnImport = &resolveRepairs
	for _, m := range mutate {
		m(cfg)
	}
	configManager := config.NewManager(cfg, "")

	coordinator := NewCoordinator(Config{
		ConfigGetter:    configManager.GetConfig,
		MetadataService: ms,
		HealthRepo:      repo,
	})

	return coordinator, ms, repo, db
}

// writeTestMetadata writes a minimal valid metadata file for the given virtual path.
func writeTestMetadata(t *testing.T, ms *metadata.MetadataService, virtualPath string) {
	t.Helper()
	seg := &metapb.SegmentData{
		Id:          "article-001@test.example.com",
		SegmentSize: 1024,
		StartOffset: 0,
		EndOffset:   1023,
	}
	meta := ms.CreateFileMetadata(
		1024, "source.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		[]*metapb.SegmentData{seg},
		metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))
}

// TestScheduleHealthCheck_ExpandsDirWrittenPaths verifies that "DIR:" written-path
// entries (RAR/7z archive imports report only their NZB folder) are expanded into
// per-file health checks, and that pending repairs in that directory are resolved.
// Before this, archive imports scheduled nothing: the literal "DIR:..." path had no
// metadata, so the import-side exit from the repair loop never fired.
func TestScheduleHealthCheck_ExpandsDirWrittenPaths(t *testing.T) {
	coordinator, ms, repo, db := setupSchedulerTest(t)
	ctx := context.Background()

	nzbFolder := "/tv/Show.S01E01.1080p-GRP"
	writeTestMetadata(t, ms, nzbFolder+"/Show.S01E01.1080p-GRP.mkv")
	writeTestMetadata(t, ms, nzbFolder+"/Show.S01E01.1080p-GRP.en.srt")

	// A stale repair record in the same directory — the new import replaces it.
	_, err := db.Exec(`
		INSERT INTO file_health (file_path, status, repair_retry_count, max_repair_retries)
		VALUES ('tv/Show.S01E01.1080p-GRP/old-broken.mkv', 'repair_triggered', 1, 3)
	`)
	require.NoError(t, err)

	err = coordinator.ScheduleHealthCheck(ctx, nil, nzbFolder, []string{"DIR:" + nzbFolder})
	require.NoError(t, err)

	// Both extracted files must have pending health records.
	for _, p := range []string{
		"tv/Show.S01E01.1080p-GRP/Show.S01E01.1080p-GRP.mkv",
		"tv/Show.S01E01.1080p-GRP/Show.S01E01.1080p-GRP.en.srt",
	} {
		h, err := repo.GetFileHealth(ctx, p)
		require.NoError(t, err)
		require.NotNil(t, h, "expected a health record for %s", p)
		assert.Equal(t, database.HealthStatusPending, h.Status)
	}

	// The stale repair record in the directory must be resolved (deleted).
	stale, err := repo.GetFileHealth(ctx, "tv/Show.S01E01.1080p-GRP/old-broken.mkv")
	require.NoError(t, err)
	assert.Nil(t, stale, "pending repair in the import directory must be resolved by the new import")
}

// TestScheduleHealthCheck_ExpandsNestedDirs verifies recursion into subdirectories
// (e.g. Blu-ray structures produced by ISO expansion inside an archive).
func TestScheduleHealthCheck_ExpandsNestedDirs(t *testing.T) {
	coordinator, ms, repo, _ := setupSchedulerTest(t)
	ctx := context.Background()

	nzbFolder := "/movies/Film.2024.1080p-GRP"
	nested := nzbFolder + "/BDMV/STREAM/00001.m2ts"
	writeTestMetadata(t, ms, nested)

	err := coordinator.ScheduleHealthCheck(ctx, nil, nzbFolder, []string{"DIR:" + nzbFolder})
	require.NoError(t, err)

	h, err := repo.GetFileHealth(ctx, "movies/Film.2024.1080p-GRP/BDMV/STREAM/00001.m2ts")
	require.NoError(t, err)
	require.NotNil(t, h, "nested files must be discovered through recursive expansion")
	assert.Equal(t, database.HealthStatusPending, h.Status)
}

// TestScheduleHealthCheck_PlainPathsUnchanged verifies non-DIR entries keep working.
func TestScheduleHealthCheck_PlainPathsUnchanged(t *testing.T) {
	coordinator, ms, repo, _ := setupSchedulerTest(t)
	ctx := context.Background()

	filePath := "/tv/Single.S01E01.mkv"
	writeTestMetadata(t, ms, filePath)

	err := coordinator.ScheduleHealthCheck(ctx, nil, filePath, []string{filePath})
	require.NoError(t, err)

	h, err := repo.GetFileHealth(ctx, "tv/Single.S01E01.mkv")
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, database.HealthStatusPending, h.Status)
}

// TestScheduleHealthCheck_DisabledQueuesNothing verifies that with health checking
// disabled in config, an import schedules no health records and does not resolve
// pending repairs — the whole health subsystem is off, so records queued here would
// only sit as "pending" forever (the worker refuses to run when disabled).
func TestScheduleHealthCheck_DisabledQueuesNothing(t *testing.T) {
	coordinator, ms, repo, db := setupSchedulerTest(t, func(cfg *config.Config) {
		disabled := false
		cfg.Health.Enabled = &disabled
	})
	ctx := context.Background()

	filePath := "/tv/Disabled.S01E01.mkv"
	writeTestMetadata(t, ms, filePath)

	// A stale repair record that would normally be resolved by an import.
	_, err := db.Exec(`
		INSERT INTO file_health (file_path, status, repair_retry_count, max_repair_retries)
		VALUES ('tv/old-broken.mkv', 'repair_triggered', 1, 3)
	`)
	require.NoError(t, err)

	err = coordinator.ScheduleHealthCheck(ctx, nil, filePath, []string{filePath})
	require.NoError(t, err)

	// No health record must be queued for the imported file.
	h, err := repo.GetFileHealth(ctx, "tv/Disabled.S01E01.mkv")
	require.NoError(t, err)
	assert.Nil(t, h, "no health record may be queued when health checking is disabled")

	// Repair resolution must be skipped too — the stale record stays untouched.
	stale, err := repo.GetFileHealth(ctx, "tv/old-broken.mkv")
	require.NoError(t, err)
	require.NotNil(t, stale, "pending repair must not be resolved when health checking is disabled")
}

// TestScheduleHealthCheck_SkipsSidecarsUnderSymlinkStrategy verifies that under
// SYMLINK/STRM strategies only ARR-importable media files are scheduled. Sidecars
// (.nfo, .srt, ...) can never be relinked to a real library path by a Download
// webhook, so their records would sit permanently ineligible for the health worker
// (GetUnhealthyFiles filters on library_path) and pile up as stuck "pending" until
// the next library sync deletes them.
func TestScheduleHealthCheck_SkipsSidecarsUnderSymlinkStrategy(t *testing.T) {
	coordinator, ms, repo, _ := setupSchedulerTest(t, func(cfg *config.Config) {
		cfg.Import.ImportStrategy = config.ImportStrategySYMLINK
	})
	ctx := context.Background()

	nzbFolder := "/tv/Show.S01E02.1080p-GRP"
	writeTestMetadata(t, ms, nzbFolder+"/Show.S01E02.1080p-GRP.mkv")
	writeTestMetadata(t, ms, nzbFolder+"/Show.S01E02.1080p-GRP.nfo")
	writeTestMetadata(t, ms, nzbFolder+"/Show.S01E02.1080p-GRP.en.srt")

	err := coordinator.ScheduleHealthCheck(ctx, nil, nzbFolder, []string{"DIR:" + nzbFolder})
	require.NoError(t, err)

	h, err := repo.GetFileHealth(ctx, "tv/Show.S01E02.1080p-GRP/Show.S01E02.1080p-GRP.mkv")
	require.NoError(t, err)
	require.NotNil(t, h, "media file must be scheduled")
	assert.Equal(t, database.HealthStatusPending, h.Status)

	for _, sidecar := range []string{
		"tv/Show.S01E02.1080p-GRP/Show.S01E02.1080p-GRP.nfo",
		"tv/Show.S01E02.1080p-GRP/Show.S01E02.1080p-GRP.en.srt",
	} {
		h, err := repo.GetFileHealth(ctx, sidecar)
		require.NoError(t, err)
		assert.Nil(t, h, "sidecar %s must not get a permanently-ineligible health record", sidecar)
	}
}

// TestScheduleHealthCheck_KeepsSidecarsUnderNoneStrategy verifies that under the NONE
// strategy (no library filter — every record is eligible as-is) sidecars keep getting
// health checks, preserving the pre-filter behavior.
func TestScheduleHealthCheck_KeepsSidecarsUnderNoneStrategy(t *testing.T) {
	coordinator, ms, repo, _ := setupSchedulerTest(t)
	ctx := context.Background()

	nzbFolder := "/tv/Show.S01E03.1080p-GRP"
	writeTestMetadata(t, ms, nzbFolder+"/Show.S01E03.1080p-GRP.mkv")
	writeTestMetadata(t, ms, nzbFolder+"/Show.S01E03.1080p-GRP.nfo")

	err := coordinator.ScheduleHealthCheck(ctx, nil, nzbFolder, []string{"DIR:" + nzbFolder})
	require.NoError(t, err)

	for _, p := range []string{
		"tv/Show.S01E03.1080p-GRP/Show.S01E03.1080p-GRP.mkv",
		"tv/Show.S01E03.1080p-GRP/Show.S01E03.1080p-GRP.nfo",
	} {
		h, err := repo.GetFileHealth(ctx, p)
		require.NoError(t, err)
		require.NotNil(t, h, "expected a health record for %s under NONE strategy", p)
		assert.Equal(t, database.HealthStatusPending, h.Status)
	}
}
