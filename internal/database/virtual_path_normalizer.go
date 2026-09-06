package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// virtualPathsNormalizedKey is the system_state marker set once every legacy
// virtual path has been rewritten to canonical form. Steady-state boots pay one
// indexed lookup and skip the table scans entirely.
const virtualPathsNormalizedKey = "virtual_paths_normalized"

// virtualPathBatchSize is the primary-key span rewritten per statement. Batching
// by id range keeps each UPDATE to one index range scan (no per-batch rescans of
// the dirty set), bounds WAL/dead-tuple growth per transaction, and gives the
// progress log something to report on large libraries.
var virtualPathBatchSize int64 = 20000

const progressLogInterval = 5 * time.Second

// canonicalPathExpr returns the SQL expression that canonicalizes a virtual path
// column the same way normalizeHealthPath does in Go: backslashes to forward
// slashes, leading and trailing separators removed.
func canonicalPathExpr(d Dialect, col string) string {
	if d == DialectPostgres {
		return fmt.Sprintf("btrim(replace(%s, chr(92), '/'), '/')", col)
	}
	return fmt.Sprintf("trim(replace(%s, char(92), '/'), '/')", col)
}

// normalizeVirtualPaths rewrites legacy import_history.virtual_path and
// file_health.file_path values to canonical form so the exact-equality reads in
// the repositories match them. It runs at startup after migrations, is
// idempotent, and after its first successful run is a single indexed lookup.
//
// This lives in Go rather than a goose data migration so a large library gets
// batched writes with progress logging instead of one silent multi-second
// transaction, and so a skipped or misapplied migration cannot leave reads
// unable to see legacy rows (which would let library_sync delete live symlinks
// as orphaned).
func normalizeVirtualPaths(ctx context.Context, sqlDB *sql.DB, d Dialect) error {
	db := newDialectAwareDB(sqlDB, d)

	var marker string
	err := db.QueryRowContext(ctx, "SELECT value FROM system_state WHERE key = ?", virtualPathsNormalizedKey).Scan(&marker)
	switch {
	case err == nil && marker == "1":
		return nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("failed to read virtual path marker: %w", err)
	}

	started := time.Now()
	historyDirty, err := countDirtyPaths(ctx, db, d, "import_history", "virtual_path")
	if err != nil {
		return err
	}
	healthDirty, err := countDirtyPaths(ctx, db, d, "file_health", "file_path")
	if err != nil {
		return err
	}
	total := historyDirty + healthDirty
	if total > 0 {
		slog.InfoContext(ctx, "Normalizing legacy virtual paths; this runs once and may take a while on large libraries",
			"import_history_rows", historyDirty, "file_health_rows", healthDirty)
	}

	if historyDirty > 0 {
		if err := normalizePathColumnInBatches(ctx, db, d, "import_history", "virtual_path", historyDirty); err != nil {
			return err
		}
	}
	if healthDirty > 0 {
		if err := normalizeFileHealthPaths(ctx, db, d, healthDirty); err != nil {
			return err
		}
	}
	// Always re-assert the trigger: a run interrupted after dropping it but
	// before restoring it must not leave file_health without updated_at maintenance.
	if err := ensureFileHealthTimestampTrigger(ctx, db, d); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO system_state (key, value, updated_at) VALUES (?, '1', CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = '1', updated_at = CURRENT_TIMESTAMP`,
		virtualPathsNormalizedKey); err != nil {
		return fmt.Errorf("failed to record virtual path normalization: %w", err)
	}
	if total > 0 {
		slog.InfoContext(ctx, "Virtual path normalization complete", "rows", total, "duration", time.Since(started))
	}
	return nil
}

func countDirtyPaths(ctx context.Context, db *dialectAwareDB, d Dialect, table, col string) (int64, error) {
	var n int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s <> %s", table, col, canonicalPathExpr(d, col))
	if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
		return 0, fmt.Errorf("failed to count non-canonical %s.%s rows: %w", table, col, err)
	}
	return n, nil
}

// normalizePathColumnInBatches rewrites dirty rows in place, walking the primary
// key range in fixed windows. The caller must guarantee no two rows canonicalize
// to the same value if the column is unique.
func normalizePathColumnInBatches(ctx context.Context, db *dialectAwareDB, d Dialect, table, col string, expected int64) error {
	var minID, maxID sql.NullInt64
	if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT MIN(id), MAX(id) FROM %s", table)).Scan(&minID, &maxID); err != nil {
		return fmt.Errorf("failed to read %s id range: %w", table, err)
	}
	if !minID.Valid {
		return nil
	}

	canon := canonicalPathExpr(d, col)
	update := fmt.Sprintf("UPDATE %s SET %s = %s WHERE id BETWEEN ? AND ? AND %s <> %s", table, col, canon, col, canon)

	var done int64
	lastLog := time.Now()
	for lo := minID.Int64; lo <= maxID.Int64; lo += virtualPathBatchSize {
		res, err := db.ExecContext(ctx, update, lo, lo+virtualPathBatchSize-1)
		if err != nil {
			return fmt.Errorf("failed to normalize %s.%s batch starting at id %d: %w", table, col, lo, err)
		}
		n, _ := res.RowsAffected()
		done += n
		if time.Since(lastLog) >= progressLogInterval {
			slog.InfoContext(ctx, "Virtual path normalization progress", "table", table, "rows_done", done, "rows_total", expected)
			lastLog = time.Now()
		}
	}
	return nil
}

// normalizeFileHealthPaths handles file_health, whose file_path is UNIQUE. Rows
// that would canonicalize to the same path are first collapsed to one survivor
// (most severe status, then newest, then highest id); the remaining dirty rows
// then have exclusive targets and are rewritten in place. Health records are
// regenerable by the health worker, so a field-by-field merge of the losers is
// not worth its complexity.
func normalizeFileHealthPaths(ctx context.Context, db *dialectAwareDB, d Dialect, expected int64) error {
	canon := canonicalPathExpr(d, "file_path")
	res, err := db.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM file_health WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY %[1]s
					ORDER BY CASE status
						WHEN 'corrupted' THEN 6
						WHEN 'repair_triggered' THEN 5
						WHEN 'degraded' THEN 4
						WHEN 'checking' THEN 3
						WHEN 'pending' THEN 2
						WHEN 'healthy' THEN 1
						ELSE 0
					END DESC, updated_at DESC, id DESC) AS rn
				FROM file_health
				WHERE %[1]s IN (
					SELECT %[1]s FROM file_health GROUP BY %[1]s HAVING COUNT(*) > 1)
			) ranked WHERE rn > 1)`, canon))
	if err != nil {
		return fmt.Errorf("failed to collapse colliding file_health paths: %w", err)
	}
	if removed, _ := res.RowsAffected(); removed > 0 {
		slog.WarnContext(ctx, "Collapsed duplicate file_health records that differed only by path separators; kept the most severe/newest of each", "removed", removed)
	}

	// A path-only rewrite must not bump updated_at, so the timestamp trigger is
	// suspended for the duration and restored by the caller.
	if err := dropFileHealthTimestampTrigger(ctx, db, d); err != nil {
		return err
	}
	return normalizePathColumnInBatches(ctx, db, d, "file_health", "file_path", expected)
}

func dropFileHealthTimestampTrigger(ctx context.Context, db *dialectAwareDB, d Dialect) error {
	stmt := "DROP TRIGGER IF EXISTS update_file_health_timestamp"
	if d == DialectPostgres {
		stmt += " ON file_health"
	}
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("failed to suspend file_health timestamp trigger: %w", err)
	}
	return nil
}

func ensureFileHealthTimestampTrigger(ctx context.Context, db *dialectAwareDB, d Dialect) error {
	if d == DialectPostgres {
		if err := dropFileHealthTimestampTrigger(ctx, db, d); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `
			CREATE TRIGGER update_file_health_timestamp
			BEFORE UPDATE ON file_health
			FOR EACH ROW EXECUTE FUNCTION update_file_health_timestamp()`); err != nil {
			return fmt.Errorf("failed to restore file_health timestamp trigger: %w", err)
		}
		return nil
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER IF NOT EXISTS update_file_health_timestamp
		AFTER UPDATE ON file_health
		BEGIN
			UPDATE file_health SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END`); err != nil {
		return fmt.Errorf("failed to restore file_health timestamp trigger: %w", err)
	}
	return nil
}
