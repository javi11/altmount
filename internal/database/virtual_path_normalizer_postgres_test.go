package database

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Runs only when ALTMOUNT_TEST_PG_DSN points at a disposable PostgreSQL
// database, e.g.
//
//	docker run -d -e POSTGRES_PASSWORD=pg -e POSTGRES_DB=altmount -p 55432:5432 postgres:16-alpine
//	ALTMOUNT_TEST_PG_DSN='postgres://postgres:pg@localhost:55432/altmount?sslmode=disable' go test ./internal/database -run Postgres
//
// The schema is dropped and recreated on every run.
func openPostgresLatest(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("ALTMOUNT_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("ALTMOUNT_TEST_PG_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public")
	require.NoError(t, err)

	goose.SetBaseFS(embedMigrations)
	require.NoError(t, goose.SetDialect("postgres"))
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.Up(db, "migrations/postgres"))
	return db
}

func TestNormalizeVirtualPaths_Postgres(t *testing.T) {
	ctx := context.Background()
	db := openPostgresLatest(t)

	_, err := db.Exec(`
		INSERT INTO import_history (id, nzb_name, file_name, virtual_path) VALUES
			(1, 'n', 'a.mkv', '/movies/a.mkv'),
			(2, 'n', 'b.mkv', chr(92) || 'shows' || chr(92) || 'b.mkv' || chr(92)),
			(3, 'n', 'c.mkv', 'clean/c.mkv');
		INSERT INTO file_health (id, file_path, status, library_path, updated_at) VALUES
			(10, '/movies/a.mkv', 'healthy', NULL, '2026-08-22 10:00:00'),
			(11, chr(92) || 'shows' || chr(92) || 'b.mkv', 'corrupted', NULL, '2026-08-22 10:00:00'),
			(12, 'clean/c.mkv', 'healthy', NULL, '2026-08-22 10:00:00'),
			(20, '/movies/x.mkv', 'corrupted', '/lib/legacy.mkv', '2026-08-20 10:00:00'),
			(21, 'movies/x.mkv', 'healthy', '/lib/canonical.mkv', '2026-08-21 10:00:00'),
			(22, chr(92) || 'movies' || chr(92) || 'x.mkv', 'healthy', NULL, '2026-08-22 10:00:00')`)
	require.NoError(t, err)

	require.NoError(t, normalizeVirtualPaths(ctx, db, DialectPostgres))

	var path string
	require.NoError(t, db.QueryRow("SELECT virtual_path FROM import_history WHERE id = 2").Scan(&path))
	assert.Equal(t, "shows/b.mkv", path)

	var id int64
	var updatedAt string
	require.NoError(t, db.QueryRow(
		"SELECT id, to_char(updated_at, 'YYYY-MM-DD HH24:MI:SS') FROM file_health WHERE file_path = 'movies/a.mkv'").Scan(&id, &updatedAt))
	assert.Equal(t, int64(10), id)
	assert.Equal(t, "2026-08-22 10:00:00", updatedAt, "path-only rewrite must not bump updated_at")

	var status string
	require.NoError(t, db.QueryRow("SELECT id, status FROM file_health WHERE file_path = 'movies/x.mkv'").Scan(&id, &status))
	assert.Equal(t, int64(20), id, "most severe row survives the collision")
	assert.Equal(t, "corrupted", status)

	var total, dirty int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM file_health").Scan(&total))
	assert.Equal(t, 4, total)
	require.NoError(t, db.QueryRow(`
		SELECT (SELECT COUNT(*) FROM file_health WHERE left(file_path, 1) = '/' OR position(chr(92) IN file_path) > 0)
		     + (SELECT COUNT(*) FROM import_history WHERE left(virtual_path, 1) = '/' OR position(chr(92) IN virtual_path) > 0)`).Scan(&dirty))
	assert.Zero(t, dirty)

	var marker string
	require.NoError(t, db.QueryRow("SELECT value FROM system_state WHERE key = $1", virtualPathsNormalizedKey).Scan(&marker))
	assert.Equal(t, "1", marker)

	// Trigger restored: an ordinary update bumps updated_at again.
	_, err = db.Exec("UPDATE file_health SET status = 'pending' WHERE id = 12")
	require.NoError(t, err)
	require.NoError(t, db.QueryRow("SELECT to_char(updated_at, 'YYYY-MM-DD HH24:MI:SS') FROM file_health WHERE id = 12").Scan(&updatedAt))
	assert.NotEqual(t, "2026-08-22 10:00:00", updatedAt)

	// Repository round-trip through the exact-equality read path.
	found, err := NewHealthRepository(db, DialectPostgres).HasImportHistoryForPath(ctx, `\movies\a.mkv\`)
	require.NoError(t, err)
	assert.True(t, found)

	require.NoError(t, normalizeVirtualPaths(ctx, db, DialectPostgres), "second run is a no-op")
}
