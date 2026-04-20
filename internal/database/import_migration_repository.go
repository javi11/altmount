package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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

// MarkSymlinksMigrated sets status=symlinks_migrated for the given row IDs.
func (r *ImportMigrationRepository) MarkSymlinksMigrated(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids)+1)
	args[0] = time.Now()
	for i, id := range ids {
		placeholders[i] = "?"
		args[i+1] = id
	}

	query := fmt.Sprintf(`
		UPDATE import_migrations
		SET status = 'symlinks_migrated', updated_at = datetime('now')
		WHERE id IN (%s)
	`, strings.Join(placeholders, ", "))

	_, err := r.db.ExecContext(ctx, query, args[1:]...)
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
			SUM(CASE WHEN status = 'pending'           THEN 1 ELSE 0 END) AS pending,
			SUM(CASE WHEN status = 'imported'          THEN 1 ELSE 0 END) AS imported,
			SUM(CASE WHEN status = 'failed'            THEN 1 ELSE 0 END) AS failed,
			SUM(CASE WHEN status = 'symlinks_migrated' THEN 1 ELSE 0 END) AS symlinks_migrated
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

	var nzbdavIDStruct struct {
		NzbdavID string `json:"nzbdav_id"`
	}

	inserted := 0
	for _, c := range candidates {
		if err := json.Unmarshal([]byte(c.metadata), &nzbdavIDStruct); err != nil {
			// Row has metadata but no parseable nzbdav_id — skip silently.
			continue
		}
		if nzbdavIDStruct.NzbdavID == "" {
			continue
		}

		relativePath := ""
		if c.relativePath != nil {
			relativePath = *c.relativePath
		}

		insertQuery := `
			INSERT OR IGNORE INTO import_migrations
				(source, external_id, queue_item_id, relative_path, final_path, status, created_at, updated_at)
			VALUES ('nzbdav', ?, ?, ?, ?, 'imported', datetime('now'), datetime('now'))
		`
		if r.dialect.IsPostgres() {
			insertQuery = `
				INSERT INTO import_migrations
					(source, external_id, queue_item_id, relative_path, final_path, status, created_at, updated_at)
				VALUES ('nzbdav', $1, $2, $3, $4, 'imported', NOW(), NOW())
				ON CONFLICT (source, external_id) DO NOTHING
			`
		}

		var res sql.Result
		if r.dialect.IsPostgres() {
			res, err = r.db.ExecContext(ctx, insertQuery, nzbdavIDStruct.NzbdavID, c.id, relativePath, c.storagePath)
		} else {
			res, err = r.db.ExecContext(ctx, insertQuery, nzbdavIDStruct.NzbdavID, c.id, relativePath, c.storagePath)
		}
		if err != nil {
			return inserted, fmt.Errorf("backfill: insert import_migration (external_id=%s): %w", nzbdavIDStruct.NzbdavID, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
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
