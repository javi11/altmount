package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openLatest(t *testing.T) *sql.DB {
	t.Helper()
	return openMigratedTo(t, 34)
}

func virtualPathsMarker(t *testing.T, db *sql.DB) string {
	t.Helper()
	var value string
	err := db.QueryRow("SELECT value FROM system_state WHERE key = ?", virtualPathsNormalizedKey).Scan(&value)
	if err == sql.ErrNoRows {
		return ""
	}
	require.NoError(t, err)
	return value
}

func TestNormalizeVirtualPaths_CanonicalizesDirtyRowsInPlace(t *testing.T) {
	ctx := context.Background()
	db := openLatest(t)

	_, err := db.Exec(`
		INSERT INTO import_history (id, nzb_name, file_name, virtual_path) VALUES
			(1, 'n', 'a.mkv', '/movies/a.mkv'),
			(2, 'n', 'b.mkv', char(92) || 'shows' || char(92) || 'b.mkv' || char(92)),
			(3, 'n', 'c.mkv', 'clean/c.mkv');
		INSERT INTO file_health (id, file_path, status, updated_at) VALUES
			(10, '/movies/a.mkv', 'healthy', '2026-08-22 10:00:00'),
			(11, char(92) || 'shows' || char(92) || 'b.mkv', 'corrupted', '2026-08-22 10:00:00'),
			(12, 'clean/c.mkv', 'healthy', '2026-08-22 10:00:00')`)
	require.NoError(t, err)

	require.NoError(t, normalizeVirtualPaths(ctx, db, DialectSQLite))

	var path string
	require.NoError(t, db.QueryRow("SELECT virtual_path FROM import_history WHERE id = 1").Scan(&path))
	assert.Equal(t, "movies/a.mkv", path)
	require.NoError(t, db.QueryRow("SELECT virtual_path FROM import_history WHERE id = 2").Scan(&path))
	assert.Equal(t, "shows/b.mkv", path)

	var id int64
	var updatedAt string
	require.NoError(t, db.QueryRow(
		"SELECT id, CAST(updated_at AS TEXT) FROM file_health WHERE file_path = 'movies/a.mkv'").Scan(&id, &updatedAt))
	assert.Equal(t, int64(10), id, "rows are rewritten in place and keep their ID")
	assert.Equal(t, "2026-08-22 10:00:00", updatedAt, "a path-only rewrite must not bump updated_at")
	require.NoError(t, db.QueryRow("SELECT id FROM file_health WHERE file_path = 'shows/b.mkv'").Scan(&id))
	assert.Equal(t, int64(11), id)

	var dirty int
	require.NoError(t, db.QueryRow(`
		SELECT (SELECT COUNT(*) FROM file_health WHERE substr(file_path,1,1) = '/' OR instr(file_path, char(92)) > 0)
		     + (SELECT COUNT(*) FROM import_history WHERE substr(virtual_path,1,1) = '/' OR instr(virtual_path, char(92)) > 0)`).Scan(&dirty))
	assert.Zero(t, dirty)
	assert.Equal(t, "1", virtualPathsMarker(t, db))

	// The updated_at trigger must be back in place afterwards.
	_, err = db.Exec("UPDATE file_health SET status = 'pending' WHERE id = 12")
	require.NoError(t, err)
	require.NoError(t, db.QueryRow("SELECT CAST(updated_at AS TEXT) FROM file_health WHERE id = 12").Scan(&updatedAt))
	assert.NotEqual(t, "2026-08-22 10:00:00", updatedAt, "trigger must be restored after normalization")
}

func TestNormalizeVirtualPaths_CollisionKeepsMostSevereNewestRow(t *testing.T) {
	ctx := context.Background()
	db := openLatest(t)

	_, err := db.Exec(`
		INSERT INTO file_health (id, file_path, status, library_path, updated_at) VALUES
			(1, '/movies/x.mkv', 'corrupted', '/lib/legacy.mkv', '2026-08-20 10:00:00'),
			(2, 'movies/x.mkv', 'healthy', '/lib/canonical.mkv', '2026-08-21 10:00:00'),
			(3, char(92) || 'movies' || char(92) || 'x.mkv', 'healthy', NULL, '2026-08-22 10:00:00'),
			(4, '/tv/y.mkv', 'healthy', NULL, '2026-08-22 10:00:00'),
			(5, 'tv/y.mkv', 'healthy', NULL, '2026-08-23 10:00:00')`)
	require.NoError(t, err)

	require.NoError(t, normalizeVirtualPaths(ctx, db, DialectSQLite))

	var total int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM file_health").Scan(&total))
	assert.Equal(t, 2, total, "each collision group collapses to one row")

	var id int64
	var status string
	require.NoError(t, db.QueryRow(
		"SELECT id, status FROM file_health WHERE file_path = 'movies/x.mkv'").Scan(&id, &status))
	assert.Equal(t, int64(1), id, "the most severe row wins even if it was the dirty one")
	assert.Equal(t, "corrupted", status)

	require.NoError(t, db.QueryRow("SELECT id FROM file_health WHERE file_path = 'tv/y.mkv'").Scan(&id))
	assert.Equal(t, int64(5), id, "equal severity: the newest row wins")
}

func TestNormalizeVirtualPaths_CleanCatalogOnlySetsMarker(t *testing.T) {
	ctx := context.Background()
	db := openLatest(t)

	_, err := db.Exec(`INSERT INTO file_health (id, file_path, status, updated_at) VALUES (1, 'clean/a.mkv', 'healthy', '2026-08-22 10:00:00')`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TEMP TABLE before AS SELECT * FROM file_health`)
	require.NoError(t, err)

	require.NoError(t, normalizeVirtualPaths(ctx, db, DialectSQLite))

	var changed int
	require.NoError(t, db.QueryRow(`
		SELECT (SELECT COUNT(*) FROM (SELECT * FROM before EXCEPT SELECT * FROM file_health))
		     + (SELECT COUNT(*) FROM (SELECT * FROM file_health EXCEPT SELECT * FROM before))`).Scan(&changed))
	assert.Zero(t, changed)
	assert.Equal(t, "1", virtualPathsMarker(t, db))
}

func TestNormalizeVirtualPaths_SkipsWhenMarkerSet(t *testing.T) {
	ctx := context.Background()
	db := openLatest(t)

	_, err := db.Exec(`
		INSERT INTO system_state (key, value) VALUES (?, '1');
		INSERT INTO file_health (id, file_path, status) VALUES (1, '/still/dirty.mkv', 'healthy')`,
		virtualPathsNormalizedKey)
	require.NoError(t, err)

	require.NoError(t, normalizeVirtualPaths(ctx, db, DialectSQLite))

	var path string
	require.NoError(t, db.QueryRow("SELECT file_path FROM file_health WHERE id = 1").Scan(&path))
	assert.Equal(t, "/still/dirty.mkv", path, "marker set means the scan is skipped entirely")
}

func TestNormalizeVirtualPaths_BatchesAcrossIDRange(t *testing.T) {
	ctx := context.Background()
	db := openLatest(t)

	old := virtualPathBatchSize
	virtualPathBatchSize = 100
	t.Cleanup(func() { virtualPathBatchSize = old })

	// Sparse IDs so a batch window covers fewer rows than its ID span; some
	// windows in the middle contain no rows at all.
	const rows = 350
	_, err := db.Exec(`
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO file_health (id, file_path, status)
		SELECT i * 3, '/dirty/' || i || '.mkv', 'healthy' FROM n`, rows)
	require.NoError(t, err)
	_, err = db.Exec(`
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO import_history (id, nzb_name, file_name, virtual_path)
		SELECT i * 3, 'n', i || '.mkv', '/dirty/' || i || '.mkv' FROM n`, rows)
	require.NoError(t, err)

	require.NoError(t, normalizeVirtualPaths(ctx, db, DialectSQLite))

	var dirty, total int
	require.NoError(t, db.QueryRow(`
		SELECT (SELECT COUNT(*) FROM file_health WHERE substr(file_path,1,1) = '/')
		     + (SELECT COUNT(*) FROM import_history WHERE substr(virtual_path,1,1) = '/')`).Scan(&dirty))
	require.NoError(t, db.QueryRow(`
		SELECT (SELECT COUNT(*) FROM file_health) + (SELECT COUNT(*) FROM import_history)`).Scan(&total))
	assert.Zero(t, dirty)
	assert.Equal(t, 2*rows, total)
}

func TestVirtualPathRepositoryUsesTheCanonicalForm(t *testing.T) {
	db := openLatest(t)
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

func TestNormalizeVirtualPaths_LargeDirtyCatalogTiming(t *testing.T) {
	ctx := context.Background()
	db := openLatest(t)

	const rows = 100000
	_, err := db.Exec(`
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO file_health (file_path, status, updated_at)
		SELECT '/dirty/' || i || '.mkv', 'healthy', '2026-08-22 10:00:00' FROM n`, rows)
	require.NoError(t, err)
	_, err = db.Exec(`
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO import_history (nzb_name, file_name, virtual_path)
		SELECT 'n', i || '.mkv', '/dirty/' || i || '.mkv' FROM n`, rows)
	require.NoError(t, err)

	started := time.Now()
	require.NoError(t, normalizeVirtualPaths(ctx, db, DialectSQLite))
	t.Logf("all-dirty %d file_health + %d import_history rows: %s", rows, rows, time.Since(started))

	started = time.Now()
	require.NoError(t, normalizeVirtualPaths(ctx, db, DialectSQLite))
	t.Logf("marker-set rerun: %s", time.Since(started))

	_, err = db.Exec("DELETE FROM system_state WHERE key = ?", virtualPathsNormalizedKey)
	require.NoError(t, err)
	started = time.Now()
	require.NoError(t, normalizeVirtualPaths(ctx, db, DialectSQLite))
	t.Logf("clean catalog without marker (dirty-count scans only): %s", time.Since(started))

	var dirty int
	require.NoError(t, db.QueryRow(`
		SELECT (SELECT COUNT(*) FROM file_health WHERE substr(file_path,1,1) = '/')
		     + (SELECT COUNT(*) FROM import_history WHERE substr(virtual_path,1,1) = '/')`).Scan(&dirty))
	assert.Zero(t, dirty)
}

// The SQL expression and normalizeHealthPath must agree on every input: the
// dirty-row count is computed in SQL, but reads compare against the Go form. If
// they diverge, the normalizer reports a clean catalog while lookups still miss.
func TestCanonicalPathExpr_MatchesGoNormalization(t *testing.T) {
	db := openLatest(t)
	inputs := []string{
		"movies/a.mkv", "/movies/a.mkv", "//movies//a.mkv//", `\movies\a.mkv`, `\\server\share\a.mkv`,
		"movies\\sub/a.mkv", "/", `\`, "", "a", "/a/", "C:\\media\\a.mkv", "tv/show/", "tv/sh%w_/a.mkv",
		"ünïcödé/ファイル.mkv", "trailing.slash.only/",
	}
	for _, in := range inputs {
		var got string
		require.NoError(t, db.QueryRow(
			"SELECT "+canonicalPathExpr(DialectSQLite, "?"), in).Scan(&got), "input %q", in)
		assert.Equal(t, normalizeHealthPath(in), got, "input %q", in)
	}
}
