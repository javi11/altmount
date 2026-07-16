package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openMigratedTo opens a fresh file-backed SQLite DB (so every pooled connection
// sees the same schema) and applies goose migrations up to the given version.
func openMigratedTo(t *testing.T, version int64) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	goose.SetBaseFS(embedMigrations)
	require.NoError(t, goose.SetDialect("sqlite3"))
	goose.SetLogger(goose.NopLogger())

	require.NoError(t, goose.UpTo(db, "migrations/sqlite", version))
	return db
}

// TestMigration034_BackfillsDownloadID verifies that migration 034 correctly
// populates file_health.download_id from import_history, matching on paths after
// normalizing leading/trailing slashes, and leaves unmatched rows NULL.
func TestMigration034_BackfillsDownloadID(t *testing.T) {
	ctx := context.Background()

	// Apply everything up to (but not including) 034.
	db := openMigratedTo(t, 33)

	// Seed import_history with the download_id "source of truth" for each path.
	_, err := db.ExecContext(ctx, `
		INSERT INTO import_history (nzb_name, file_name, virtual_path, download_id) VALUES
			('n', 'a.mkv', '/movies/a.mkv',  'dl-a'),
			('n', 'b.mkv', 'shows/b.mkv/',   'dl-b'),
			('n', 'd.mkv', '/tv/d.mkv/',     'dl-d')
	`)
	require.NoError(t, err)

	// Seed file_health rows whose paths differ from import_history only in
	// leading/trailing slashes, plus one row with no matching history.
	_, err = db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status) VALUES
			('movies/a.mkv',  'pending'),
			('/shows/b.mkv',  'pending'),
			('tv/d.mkv',      'pending'),
			('movies/none.mkv','pending')
	`)
	require.NoError(t, err)

	// Apply migration 034 (adds download_id column + backfill).
	require.NoError(t, goose.UpTo(db, "migrations/sqlite", 34))

	tests := []struct {
		name     string
		filePath string
		want     sql.NullString
	}{
		{"leading-slash difference", "movies/a.mkv", sql.NullString{String: "dl-a", Valid: true}},
		{"trailing-slash difference", "/shows/b.mkv", sql.NullString{String: "dl-b", Valid: true}},
		{"both-slash difference", "tv/d.mkv", sql.NullString{String: "dl-d", Valid: true}},
		{"no history match stays NULL", "movies/none.mkv", sql.NullString{Valid: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got sql.NullString
			err := db.QueryRowContext(ctx,
				"SELECT download_id FROM file_health WHERE file_path = ?", tt.filePath).Scan(&got)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	// The temporary backfill index must not survive the migration.
	var idxCount int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_import_history_trim_vpath'").Scan(&idxCount))
	assert.Equal(t, 0, idxCount, "temporary backfill index should be dropped by the migration")
}
func TestRunMigrations_ReconcilesExistingDownloadIDColumn(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTo(t, 33)

	_, err := db.ExecContext(ctx, `
		INSERT INTO import_history (nzb_name, file_name, virtual_path, download_id)
		VALUES ('n', 'a.mkv', '/movies/a.mkv', 'dl-a');
		INSERT INTO file_health (file_path, status) VALUES ('movies/a.mkv', 'pending');
		ALTER TABLE file_health ADD COLUMN download_id TEXT DEFAULT NULL;
	`)
	require.NoError(t, err)

	require.NoError(t, runMigrations(db, DialectSQLite))

	var downloadID sql.NullString
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT download_id FROM file_health WHERE file_path = 'movies/a.mkv'").Scan(&downloadID))
	assert.Equal(t, sql.NullString{String: "dl-a", Valid: true}, downloadID)

	var applied bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM goose_db_version WHERE version_id = 34 AND is_applied = 1)").Scan(&applied))
	assert.True(t, applied)
}

// TestMigration034_BackfillUsesExpressionIndex is the regression guard for the
// startup hang: it proves that the per-row lookup performed by the backfill is a
// full table SCAN without the expression index (the O(N*M) hazard), and an
// index SEARCH once the expression index exists.
func TestMigration034_BackfillUsesExpressionIndex(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(ctx, `
		CREATE TABLE import_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			virtual_path TEXT NOT NULL,
			download_id TEXT
		);
	`)
	require.NoError(t, err)

	// This mirrors the correlated subquery executed once per file_health row.
	const lookup = `EXPLAIN QUERY PLAN
		SELECT download_id FROM import_history
		WHERE TRIM(virtual_path, '/') = TRIM(?, '/') LIMIT 1`

	explain := func() string {
		rows, err := db.QueryContext(ctx, lookup, "movies/a.mkv")
		require.NoError(t, err)
		defer rows.Close()
		var b strings.Builder
		for rows.Next() {
			var id, parent, notused int
			var detail string
			require.NoError(t, rows.Scan(&id, &parent, &notused, &detail))
			b.WriteString(detail)
			b.WriteString("\n")
		}
		require.NoError(t, rows.Err())
		return b.String()
	}

	// Without the expression index: full scan of import_history per row → O(N*M).
	before := explain()
	assert.Contains(t, before, "SCAN",
		"without the expression index the lookup must be a full scan (the hang)")
	assert.NotContains(t, before, "idx_import_history_trim_vpath")

	// With the expression index (as migration 034 now creates): index search.
	_, err = db.ExecContext(ctx,
		"CREATE INDEX idx_import_history_trim_vpath ON import_history(TRIM(virtual_path, '/'));")
	require.NoError(t, err)

	after := explain()
	assert.Contains(t, after, "idx_import_history_trim_vpath",
		"with the expression index the lookup must use it (fixes the O(N*M) hang)")
	assert.Contains(t, after, "SEARCH")
}
