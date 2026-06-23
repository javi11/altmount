package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// StoreRefRepository tracks reference counts on .nzbz store files.
// When all .meta files that reference a given store are deleted,
// the refcount hits 0 and the caller can delete the store file.
type StoreRefRepository struct {
	db      *dialectAwareDB
	dialect dialectHelper
}

// NewStoreRefRepository creates a new StoreRefRepository.
func NewStoreRefRepository(db *sql.DB, d Dialect) *StoreRefRepository {
	return &StoreRefRepository{
		db:      newDialectAwareDB(db, d),
		dialect: dialectHelper{d: d},
	}
}

// IncStoreRef increments the ref_count for storePath by 1.
// If no row exists yet, it inserts one with ref_count = 1.
func (r *StoreRefRepository) IncStoreRef(ctx context.Context, storePath string) error {
	query := r.dialect.q(`
		INSERT INTO nzb_store_refs (store_path, ref_count)
		VALUES (?, 1)
		ON CONFLICT(store_path) DO UPDATE SET
			ref_count = nzb_store_refs.ref_count + 1
	`)
	_, err := r.db.ExecContext(ctx, query, storePath)
	if err != nil {
		return fmt.Errorf("inc store ref %q: %w", storePath, err)
	}
	return nil
}

// DecStoreRef decrements the ref_count for storePath by 1.
// If the resulting count is ≤ 0, the row is deleted and 0 is returned.
// Returns the new count (0 if the row was deleted or did not exist).
func (r *StoreRefRepository) DecStoreRef(ctx context.Context, storePath string) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("dec store ref %q: begin tx: %w", storePath, err)
	}
	defer tx.Rollback()

	updateQuery := r.dialect.q(`
		UPDATE nzb_store_refs
		SET ref_count = ref_count - 1
		WHERE store_path = ?
	`)
	if _, err := tx.ExecContext(ctx, updateQuery, storePath); err != nil {
		return 0, fmt.Errorf("dec store ref %q: update: %w", storePath, err)
	}

	var count int64
	selectQuery := r.dialect.q(`SELECT ref_count FROM nzb_store_refs WHERE store_path = ?`)
	err = tx.QueryRowContext(ctx, selectQuery, storePath).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		// Row didn't exist before the decrement; nothing to do.
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("dec store ref %q: commit: %w", storePath, err)
		}
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("dec store ref %q: select: %w", storePath, err)
	}

	if count <= 0 {
		deleteQuery := r.dialect.q(`DELETE FROM nzb_store_refs WHERE store_path = ?`)
		if _, err := tx.ExecContext(ctx, deleteQuery, storePath); err != nil {
			return 0, fmt.Errorf("dec store ref %q: delete: %w", storePath, err)
		}
		count = 0
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("dec store ref %q: commit: %w", storePath, err)
	}
	return count, nil
}

// GetStoreRefCount returns the current ref_count for storePath.
// Returns 0 (and no error) if the row does not exist.
func (r *StoreRefRepository) GetStoreRefCount(ctx context.Context, storePath string) (int64, error) {
	query := r.dialect.q(`SELECT ref_count FROM nzb_store_refs WHERE store_path = ?`)
	var count int64
	err := r.db.QueryRowContext(ctx, query, storePath).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get store ref count %q: %w", storePath, err)
	}
	return count, nil
}
