package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func applyVirtualPathMigration035(t *testing.T, db *sql.DB) {
	t.Helper()
	migration, err := embedMigrations.ReadFile("migrations/sqlite/035_normalize_virtual_paths.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
}

func TestMigration035_NormalizesAndMergesHealthPathsIdempotently(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTo(t, 34)

	// These names deliberately exist in the permanent schema. Migration cleanup
	// must address only its temporary tables, never an application-owned table.
	_, err := db.ExecContext(ctx, `
		CREATE TABLE file_health_path_collisions (marker TEXT NOT NULL);
		CREATE TABLE file_health_path_affected (marker TEXT NOT NULL);
		CREATE TABLE file_health_path_merged (marker TEXT NOT NULL);
		INSERT INTO file_health_path_collisions VALUES ('permanent-collisions');
		INSERT INTO file_health_path_affected VALUES ('permanent-affected');
		INSERT INTO file_health_path_merged VALUES ('permanent-merged')`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO import_history (nzb_name, file_name, virtual_path, download_id) VALUES
			('n', 'collision.mkv', char(92) || 'movies' || char(92) || 'collision.mkv', 'history-collision'),
			('n', 'windows.mkv', '//shows/windows.mkv/', 'history-windows')
	`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO file_health (
			id, file_path, library_path, status, last_checked, last_error,
			retry_count, max_retries, repair_retry_count, max_repair_retries,
			source_nzb_path, error_details, created_at, updated_at, release_date,
			scheduled_check_at, priority, streaming_failure_count, is_masked,
			metadata, indexer, download_id
		) VALUES
			(1, '/movies/collision.mkv', '/library/legacy.mkv', 'healthy',
			 '2026-08-20 10:00:00', 'legacy error', 1, 2, 2, 3,
			 'legacy.nzb', '{"legacy":true}', '2026-08-19 10:00:00',
			 '2026-08-20 10:00:00', '2026-08-19 00:00:00',
			 '2026-08-20 09:00:00', 1, 1, FALSE, 'legacy', 'old', 'dl-old'),
			(2, 'movies/collision.mkv', '', 'corrupted',
			 '2026-08-21 10:00:00', '', 4, 2, 1, 4,
			 '', '', '2026-08-20 10:00:00', '2026-08-21 10:00:00', NULL,
			 NULL, 2, 3, TRUE, 'canonical', 'new', 'dl-new'),
			(3, char(92) || 'shows' || char(92) || 'windows.mkv' || char(92), NULL, 'degraded',
			 '2026-08-18 10:00:00', 'windows error', 0, 2, 0, 3,
			 NULL, NULL, '2026-08-18 10:00:00', '2026-08-18 10:00:00', NULL,
			 NULL, 0, 0, FALSE, NULL, NULL, NULL)
	`)
	require.NoError(t, err)

	require.NoError(t, goose.UpTo(db, "migrations/sqlite", 35))
	assertVirtualPathMigrationInvariants(t, db, 2)

	var (
		id                                                       int64
		path, status, lastError, metadata, indexer, downloadID   string
		retryCount, repairRetryCount, maxRepairRetries, priority int
		masked                                                   bool
	)
	err = db.QueryRowContext(ctx, `
		SELECT id, file_path, status, last_error, retry_count, repair_retry_count,
		       max_repair_retries, priority, is_masked, metadata, indexer, download_id
		FROM file_health WHERE file_path = 'movies/collision.mkv'
	`).Scan(&id, &path, &status, &lastError, &retryCount, &repairRetryCount,
		&maxRepairRetries, &priority, &masked, &metadata, &indexer, &downloadID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), id, "existing canonical ID must be retained")
	assert.Equal(t, "movies/collision.mkv", path)
	assert.Equal(t, "corrupted", status, "the more severe state must win")
	assert.Equal(t, "legacy error", lastError, "an empty newer error must not erase evidence")
	assert.Equal(t, 4, retryCount)
	assert.Equal(t, 2, repairRetryCount)
	assert.Equal(t, 4, maxRepairRetries)
	assert.Equal(t, 2, priority)
	assert.True(t, masked)
	assert.Equal(t, "canonical", metadata)
	assert.Equal(t, "new", indexer)
	assert.Equal(t, "dl-new", downloadID)

	var libraryPath, sourceNZBPath, errorDetails, createdAt, updatedAt, releaseDate, scheduledAt string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT library_path, source_nzb_path, error_details, CAST(created_at AS TEXT),
		       CAST(updated_at AS TEXT), CAST(release_date AS TEXT),
		       CAST(scheduled_check_at AS TEXT)
		FROM file_health WHERE file_path = 'movies/collision.mkv'`).Scan(
		&libraryPath, &sourceNZBPath, &errorDetails, &createdAt, &updatedAt,
		&releaseDate, &scheduledAt))
	assert.Equal(t, "/library/legacy.mkv", libraryPath, "non-empty library evidence survives")
	assert.Equal(t, "legacy.nzb", sourceNZBPath, "non-empty NZB evidence survives")
	assert.Equal(t, "{\"legacy\":true}", errorDetails, "non-empty error evidence survives")
	assert.Equal(t, "2026-08-19 10:00:00", createdAt, "oldest creation time survives")
	assert.Equal(t, "2026-08-21 10:00:00", updatedAt, "newest update time survives")
	assert.Equal(t, "2026-08-19 00:00:00", releaseDate, "non-empty release date survives")
	assert.Equal(t, "2026-08-20 09:00:00", scheduledAt, "earliest scheduled check survives")

	assertPermanentMigrationTablesSurvive(t, db, ctx, "migration cleanup")

	var historyPath string
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT virtual_path FROM import_history WHERE file_name = 'collision.mkv'").Scan(&historyPath))
	assert.Equal(t, "movies/collision.mkv", historyPath)
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT virtual_path FROM import_history WHERE file_name = 'windows.mkv'").Scan(&historyPath))
	assert.Equal(t, "shows/windows.mkv", historyPath)

	// A second application is safe and leaves the merged row/evidence untouched.
	applyVirtualPathMigration035(t, db)
	assertVirtualPathMigrationInvariants(t, db, 2)
	assertPermanentMigrationTablesSurvive(t, db, ctx, "repeated migration cleanup")
	var secondID int64
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT id FROM file_health WHERE file_path = 'movies/collision.mkv'").Scan(&secondID))
	assert.Equal(t, int64(2), secondID)
}

func TestMigration035_CleanCatalogIsNoOp(t *testing.T) {
	db := openMigratedTo(t, 34)

	const cleanRows = 100000
	_, err := db.Exec(`
		WITH RECURSIVE numbers(n) AS (
			SELECT 1
			UNION ALL
			SELECT n + 1 FROM numbers WHERE n < ?
		)
		INSERT INTO file_health (file_path, status, updated_at, metadata)
		SELECT 'clean/' || n || '.mkv', 'healthy', '2026-08-22 10:00:00', '{"n":' || n || '}'
		FROM numbers`, cleanRows)
	require.NoError(t, err)

	// Snapshot every column so this test catches accidental rebuilds as well as
	// path mutations. A clean catalog should not enter the affected-row table.
	_, err = db.Exec(`CREATE TEMP TABLE file_health_before AS SELECT * FROM file_health`)
	require.NoError(t, err)

	started := time.Now()
	applyVirtualPathMigration035(t, db)
	t.Logf("migration 035 clean %d-row catalog: %s", cleanRows, time.Since(started))

	var changed int
	require.NoError(t, db.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM (SELECT * FROM file_health_before EXCEPT SELECT * FROM file_health)) +
			(SELECT COUNT(*) FROM (SELECT * FROM file_health EXCEPT SELECT * FROM file_health_before))`).Scan(&changed))
	assert.Zero(t, changed, "clean file_health rows must remain byte-for-byte unchanged")

	var historyRows, healthRows int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM import_history").Scan(&historyRows))
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM file_health").Scan(&healthRows))
	assert.Zero(t, historyRows)
	assert.Equal(t, cleanRows, healthRows)
}

func assertVirtualPathMigrationInvariants(t *testing.T, db *sql.DB, wantHealthRows int) {
	t.Helper()
	var healthRows, distinctPaths, badHealthPaths, badHistoryPaths int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM file_health").Scan(&healthRows))
	require.NoError(t, db.QueryRow("SELECT COUNT(DISTINCT file_path) FROM file_health").Scan(&distinctPaths))
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM file_health WHERE substr(file_path, 1, 1) = '/' OR instr(file_path, char(92)) > 0").Scan(&badHealthPaths))
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM import_history WHERE substr(virtual_path, 1, 1) = '/' OR instr(virtual_path, char(92)) > 0").Scan(&badHistoryPaths))
	assert.Equal(t, wantHealthRows, healthRows)
	assert.Equal(t, wantHealthRows, distinctPaths)
	assert.Zero(t, badHealthPaths)
	assert.Zero(t, badHistoryPaths)
}

func assertPermanentMigrationTablesSurvive(t *testing.T, db *sql.DB, ctx context.Context, operation string) {
	t.Helper()
	for _, table := range []struct {
		name string
		want string
	}{
		{"file_health_path_collisions", "permanent-collisions"},
		{"file_health_path_affected", "permanent-affected"},
		{"file_health_path_merged", "permanent-merged"},
	} {
		var marker string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT marker FROM "+table.name).Scan(&marker))
		assert.Equal(t, table.want, marker, table.name+" must survive "+operation)
	}
}

func TestVirtualPathRepositoryUsesTheCanonicalForm(t *testing.T) {
	db := openMigratedTo(t, 35)
	ctx := context.Background()
	repo := NewRepository(db, DialectSQLite)
	health := NewHealthRepository(db, DialectSQLite)

	err := repo.AddImportHistory(ctx, &ImportHistory{
		NzbName:     "release",
		FileName:    "movie.mkv",
		VirtualPath: `\movies\movie.mkv`,
	})
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, library_path, status)
		VALUES ('movies/movie.mkv', '/library/movie.mkv', 'healthy')
	`)
	require.NoError(t, err)

	var storedPath string
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT virtual_path FROM import_history LIMIT 1").Scan(&storedPath))
	assert.Equal(t, "movies/movie.mkv", storedPath)

	history, err := repo.GetImportHistoryByPath(ctx, "//movies/movie.mkv")
	require.NoError(t, err)
	require.NotNil(t, history)
	require.NotNil(t, history.LibraryPath)
	assert.Equal(t, "/library/movie.mkv", *history.LibraryPath)

	found, err := health.HasImportHistoryForPath(ctx, `\movies\movie.mkv\`)
	require.NoError(t, err)
	assert.True(t, found)
}
