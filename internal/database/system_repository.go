package database

import (
	"context"
	"database/sql"
	"fmt"
)

// GetSystemStat retrieves a system statistic by key
func (r *Repository) GetSystemStat(ctx context.Context, key string) (int64, error) {
	query := `SELECT value FROM system_stats WHERE key = ?`
	var value int64
	err := r.db.QueryRowContext(ctx, query, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get system stat %s: %w", key, err)
	}
	return value, nil
}

// UpdateSystemStat updates a system statistic by key
func (r *Repository) UpdateSystemStat(ctx context.Context, key string, value int64) error {
	query := `
		INSERT INTO system_stats (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET
		value = excluded.value,
		updated_at = datetime('now')
	`
	_, err := r.db.ExecContext(ctx, query, key, value)
	if err != nil {
		return fmt.Errorf("failed to update system stat %s: %w", key, err)
	}
	return nil
}

// ResetSystemStats resets all cumulative system statistics to zero
func (r *Repository) ResetSystemStats(ctx context.Context) error {
	query := `UPDATE system_stats SET value = 0, updated_at = datetime('now') WHERE key != 'max_download_speed'`
	_, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to reset system stats: %w", err)
	}
	return nil
}
