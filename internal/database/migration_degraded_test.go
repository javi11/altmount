package database

import (
	"path/filepath"
	"testing"
)

// TestMigration033DegradedStatus runs the full migration chain and verifies
// the rebuilt file_health table accepts the new 'degraded' status while still
// rejecting unknown statuses, and that the timestamp trigger plus all indexes
// survived the SQLite table rebuild.
func TestMigration033DegradedStatus(t *testing.T) {
	db, err := NewDB(Config{Type: "sqlite", DatabasePath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("migration chain failed: %v", err)
	}
	conn := db.Connection()

	if _, err := conn.Exec(
		`INSERT INTO file_health (file_path, status) VALUES ('/movies/a.mkv', 'degraded')`,
	); err != nil {
		t.Fatalf("inserting a degraded row must succeed: %v", err)
	}

	if _, err := conn.Exec(
		`INSERT INTO file_health (file_path, status) VALUES ('/movies/b.mkv', 'bogus')`,
	); err == nil {
		t.Fatal("CHECK constraint should reject unknown statuses")
	}

	// The update trigger must have been recreated by the rebuild.
	var trigger int
	err = conn.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = 'update_file_health_timestamp'`,
	).Scan(&trigger)
	if err != nil || trigger != 1 {
		t.Fatalf("update_file_health_timestamp trigger missing after rebuild (count=%d, err=%v)", trigger, err)
	}

	// All pre-033 indexes must have been recreated.
	wantIndexes := []string{
		"idx_file_health_status", "idx_file_health_path", "idx_file_health_source",
		"idx_file_health_updated", "idx_file_health_library_path", "idx_file_health_masked",
		"idx_file_health_indexer", "idx_file_health_release_date", "idx_file_health_scheduled",
		"idx_file_health_due",
	}
	for _, idx := range wantIndexes {
		var n int
		err = conn.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, idx,
		).Scan(&n)
		if err != nil || n != 1 {
			t.Errorf("index %s missing after rebuild (count=%d, err=%v)", idx, n, err)
		}
	}
}
