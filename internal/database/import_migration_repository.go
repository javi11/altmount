package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// ImportMigrationRepository handles database operations for import_migrations.
type ImportMigrationRepository struct {
	db      *dialectAwareDB
	dialect dialectHelper
}

// NewImportMigrationRepository creates a new ImportMigrationRepository.
func NewImportMigrationRepository(db *sql.DB, d Dialect) *ImportMigrationRepository {
	return &ImportMigrationRepository{
		db:      newDialectAwareDB(db, d),
		dialect: dialectHelper{d: d},
	}
}

// Upsert inserts or updates a migration row keyed by (source, external_id).
// Returns the row ID.
func (r *ImportMigrationRepository) Upsert(ctx context.Context, row *ImportMigration) (int64, error) {
	query := `
		INSERT INTO import_migrations
			(source, external_id, queue_item_id, relative_path, final_path, status, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
		ON CONFLICT(source, external_id) DO UPDATE SET
			queue_item_id = COALESCE(excluded.queue_item_id, import_migrations.queue_item_id),
			relative_path = excluded.relative_path,
			final_path    = COALESCE(excluded.final_path, import_migrations.final_path),
			status        = excluded.status,
			error         = excluded.error,
			updated_at    = datetime('now')
	`
	args := []any{
		row.Source, row.ExternalID, row.QueueItemID,
		row.RelativePath, row.FinalPath, string(row.Status), row.Error,
	}

	if r.dialect.IsPostgres() {
		var id int64
		err := r.db.QueryRowContext(ctx, query+" RETURNING id", args...).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			return 0, fmt.Errorf("upsert import_migration: %w", err)
		}
		return id, nil
	}

	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("upsert import_migration: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("upsert import_migration last insert id: %w", err)
	}
	return id, nil
}

// MarkImported sets status=imported and final_path for all rows matching queue_item_id.
func (r *ImportMigrationRepository) MarkImported(ctx context.Context, queueItemID int64, finalPath string) error {
	query := `
		UPDATE import_migrations
		SET status = 'imported', final_path = ?, updated_at = datetime('now')
		WHERE queue_item_id = ?
	`
	_, err := r.db.ExecContext(ctx, query, finalPath, queueItemID)
	if err != nil {
		return fmt.Errorf("mark import_migration imported (queue_item_id=%d): %w", queueItemID, err)
	}
	return nil
}

// MarkFailed sets status=failed and error for all rows matching queue_item_id.
func (r *ImportMigrationRepository) MarkFailed(ctx context.Context, queueItemID int64, errMsg string) error {
	query := `
		UPDATE import_migrations
		SET status = 'failed', error = ?, updated_at = datetime('now')
		WHERE queue_item_id = ?
	`
	_, err := r.db.ExecContext(ctx, query, errMsg, queueItemID)
	if err != nil {
		return fmt.Errorf("mark import_migration failed (queue_item_id=%d): %w", queueItemID, err)
	}
	return nil
}

// LinkQueueItemID sets queue_item_id for all migration rows matching (source, externalIDs).
// Unconditionally overwrites any existing queue_item_id so that re-imports after a
// cancelled/failed first attempt can re-link to the new queue item. This is safe because
// IsMigrationCompleted already short-circuits rows with status=imported before we get here.
func (r *ImportMigrationRepository) LinkQueueItemID(ctx context.Context, source string, externalIDs []string, queueItemID int64) error {
	if len(externalIDs) == 0 {
		return nil
	}

	placeholders := make([]string, len(externalIDs))
	args := make([]any, 0, len(externalIDs)+2)
	args = append(args, queueItemID)
	for i, id := range externalIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, source)

	query := fmt.Sprintf(`
		UPDATE import_migrations
		SET queue_item_id = ?, updated_at = datetime('now')
		WHERE external_id IN (%s)
		AND source = ?
	`, strings.Join(placeholders, ", "))

	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("link queue_item_id (source=%s, queueItemID=%d): %w", source, queueItemID, err)
	}
	return nil
}

// MarkSymlinksMigrated sets status=symlinks_migrated for the given row IDs.
func (r *ImportMigrationRepository) MarkSymlinksMigrated(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		UPDATE import_migrations
		SET status = 'symlinks_migrated', updated_at = datetime('now')
		WHERE id IN (%s)
	`, strings.Join(placeholders, ", "))

	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mark import_migrations symlinks_migrated: %w", err)
	}
	return nil
}

// LookupByExternalID returns the migration row for (source, external_id), or nil if not found.
func (r *ImportMigrationRepository) LookupByExternalID(ctx context.Context, source, externalID string) (*ImportMigration, error) {
	query := `
		SELECT id, source, external_id, queue_item_id, relative_path, final_path, status, error, created_at, updated_at
		FROM import_migrations
		WHERE source = ? AND external_id = ?
	`
	row := r.db.QueryRowContext(ctx, query, source, externalID)
	m, err := scanImportMigration(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup import_migration (source=%s, external_id=%s): %w", source, externalID, err)
	}
	return m, nil
}

// ListByStatus returns paginated rows for source with the given status.
func (r *ImportMigrationRepository) ListByStatus(ctx context.Context, source string, status ImportMigrationStatus, limit, offset int) ([]*ImportMigration, error) {
	query := `
		SELECT id, source, external_id, queue_item_id, relative_path, final_path, status, error, created_at, updated_at
		FROM import_migrations
		WHERE source = ? AND status = ?
		ORDER BY id ASC
		LIMIT ? OFFSET ?
	`
	rows, err := r.db.QueryContext(ctx, query, source, string(status), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list import_migrations by status: %w", err)
	}
	defer rows.Close()

	var result []*ImportMigration
	for rows.Next() {
		m, err := scanImportMigrationRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan import_migration: %w", err)
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate import_migrations: %w", err)
	}
	return result, nil
}

// Stats returns aggregate counts for a source.
func (r *ImportMigrationRepository) Stats(ctx context.Context, source string) (*ImportMigrationStats, error) {
	query := `
		SELECT
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = 'pending'           THEN 1 ELSE 0 END), 0) AS pending,
			COALESCE(SUM(CASE WHEN status = 'imported'          THEN 1 ELSE 0 END), 0) AS imported,
			COALESCE(SUM(CASE WHEN status = 'failed'            THEN 1 ELSE 0 END), 0) AS failed,
			COALESCE(SUM(CASE WHEN status = 'symlinks_migrated' THEN 1 ELSE 0 END), 0) AS symlinks_migrated
		FROM import_migrations
		WHERE source = ?
	`
	var stats ImportMigrationStats
	err := r.db.QueryRowContext(ctx, query, source).Scan(
		&stats.Total,
		&stats.Pending,
		&stats.Imported,
		&stats.Failed,
		&stats.SymlinksMigrated,
	)
	if err != nil {
		return nil, fmt.Errorf("stats import_migrations (source=%s): %w", source, err)
	}
	return &stats, nil
}

// DeletePendingBySource removes all migration rows for a source that have
// status='pending'. Returns the number of rows deleted. Use this to clear
// orphaned rows from a previous import attempt so a fresh import starts clean
// (imported/symlinks_migrated rows are preserved).
func (r *ImportMigrationRepository) DeletePendingBySource(ctx context.Context, source string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM import_migrations WHERE source = ? AND status = 'pending'`, source)
	if err != nil {
		return 0, fmt.Errorf("delete pending import_migrations (source=%s): %w", source, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete pending import_migrations rows affected: %w", err)
	}
	return n, nil
}

// DeleteAllBySource removes every migration row for a source regardless of
// status. Returns the number of rows deleted. Use to force a full re-import
// after the imported files have been deleted from AltMount.
func (r *ImportMigrationRepository) DeleteAllBySource(ctx context.Context, source string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM import_migrations WHERE source = ?`, source)
	if err != nil {
		return 0, fmt.Errorf("delete all import_migrations (source=%s): %w", source, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete all import_migrations rows affected: %w", err)
	}
	return n, nil
}

// ExistsForSource returns true if any rows exist for the given source.
func (r *ImportMigrationRepository) ExistsForSource(ctx context.Context, source string) (bool, error) {
	query := `SELECT COUNT(*) FROM import_migrations WHERE source = ? LIMIT 1`
	var count int
	if err := r.db.QueryRowContext(ctx, query, source).Scan(&count); err != nil {
		return false, fmt.Errorf("exists import_migrations (source=%s): %w", source, err)
	}
	return count > 0, nil
}

// BackfillFromImportQueue reads completed import_queue rows that contain a nzbdav_id
// in their metadata JSON and inserts them as status=imported rows into import_migrations
// (idempotent via ON CONFLICT IGNORE / INSERT OR IGNORE).
// Returns the number of rows inserted.
func (r *ImportMigrationRepository) BackfillFromImportQueue(ctx context.Context) (int, error) {
	selectQuery := `
		SELECT id, relative_path, storage_path, metadata
		FROM import_queue
		WHERE status = 'completed'
		  AND metadata IS NOT NULL
		  AND storage_path IS NOT NULL
	`
	rows, err := r.db.QueryContext(ctx, selectQuery)
	if err != nil {
		return 0, fmt.Errorf("backfill: query import_queue: %w", err)
	}
	defer rows.Close()

	type row struct {
		id           int64
		relativePath *string
		storagePath  *string
		metadata     string
	}

	var candidates []row
	for rows.Next() {
		var candidate row
		if err := rows.Scan(&candidate.id, &candidate.relativePath, &candidate.storagePath, &candidate.metadata); err != nil {
			return 0, fmt.Errorf("backfill: scan import_queue row: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("backfill: iterate import_queue: %w", err)
	}

	type insertRow struct {
		externalID   string
		queueItemID  int64
		relativePath string
		storagePath  *string
	}

	var pending []insertRow
	for _, c := range candidates {
		var nzbdavIDStruct struct {
			NzbdavID string `json:"nzbdav_id"`
		}
		if err := json.Unmarshal([]byte(c.metadata), &nzbdavIDStruct); err != nil {
			continue
		}
		if nzbdavIDStruct.NzbdavID == "" {
			continue
		}
		relativePath := ""
		if c.relativePath != nil {
			relativePath = *c.relativePath
		}
		pending = append(pending, insertRow{
			externalID:   nzbdavIDStruct.NzbdavID,
			queueItemID:  c.id,
			relativePath: relativePath,
			storagePath:  c.storagePath,
		})
	}

	// Chunk to stay under SQLite's parameter limit (4 params per row × 100 = 400).
	const backfillChunk = 100
	inserted := 0
	for start := 0; start < len(pending); start += backfillChunk {
		end := min(start+backfillChunk, len(pending))
		chunk := pending[start:end]

		args := make([]any, 0, len(chunk)*4)
		valuePlaceholders := make([]string, len(chunk))
		for i, p := range chunk {
			if r.dialect.IsPostgres() {
				base := i*4 + 1
				valuePlaceholders[i] = fmt.Sprintf("('nzbdav', $%d, $%d, $%d, $%d, 'imported', NOW(), NOW())", base, base+1, base+2, base+3)
			} else {
				valuePlaceholders[i] = "('nzbdav', ?, ?, ?, ?, 'imported', datetime('now'), datetime('now'))"
			}
			args = append(args, p.externalID, p.queueItemID, p.relativePath, p.storagePath)
		}

		var query string
		if r.dialect.IsPostgres() {
			query = `INSERT INTO import_migrations
				(source, external_id, queue_item_id, relative_path, final_path, status, created_at, updated_at)
				VALUES ` + strings.Join(valuePlaceholders, ", ") + `
				ON CONFLICT (source, external_id) DO NOTHING`
		} else {
			query = `INSERT OR IGNORE INTO import_migrations
				(source, external_id, queue_item_id, relative_path, final_path, status, created_at, updated_at)
				VALUES ` + strings.Join(valuePlaceholders, ", ")
		}

		res, execErr := r.db.ExecContext(ctx, query, args...)
		if execErr != nil {
			return inserted, fmt.Errorf("backfill: bulk insert import_migrations: %w", execErr)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted += int(n)
		}
	}

	return inserted, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func scanImportMigration(row *sql.Row) (*ImportMigration, error) {
	var m ImportMigration
	var status string
	err := row.Scan(
		&m.ID, &m.Source, &m.ExternalID, &m.QueueItemID,
		&m.RelativePath, &m.FinalPath, &status, &m.Error,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	m.Status = ImportMigrationStatus(status)
	return &m, nil
}

func scanImportMigrationRow(rows *sql.Rows) (*ImportMigration, error) {
	var m ImportMigration
	var status string
	err := rows.Scan(
		&m.ID, &m.Source, &m.ExternalID, &m.QueueItemID,
		&m.RelativePath, &m.FinalPath, &status, &m.Error,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	m.Status = ImportMigrationStatus(status)
	return &m, nil
}
