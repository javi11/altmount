package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Dialect identifies the database backend.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

// dialectHelper provides SQL fragment helpers for dialect differences.
type dialectHelper struct {
	d Dialect
}

// IsPostgres reports whether the active dialect is PostgreSQL.
func (h dialectHelper) IsPostgres() bool {
	return h.d == DialectPostgres
}

// DatetimePlusHour returns an expression equivalent to "now + 1 hour".
//
//   - SQLite: datetime('now', '+1 hour')
//   - PostgreSQL: NOW() + INTERVAL '1 hour'
func (h dialectHelper) DatetimePlusHour() string {
	if h.IsPostgres() {
		return "NOW() + INTERVAL '1 hour'"
	}
	return "datetime('now', '+1 hour')"
}

// ColumnPlusMinutes returns an expression that adds n minutes to a column value.
//
//   - SQLite: datetime(col, '+N minutes')
//   - PostgreSQL: col + INTERVAL 'N minutes'
func (h dialectHelper) ColumnPlusMinutes(col string, n int) string {
	if h.IsPostgres() {
		return fmt.Sprintf("%s + INTERVAL '%d minutes'", col, n)
	}
	return fmt.Sprintf("datetime(%s, '+%d minutes')", col, n)
}

// JSONExtract returns an expression that extracts a top-level key from a JSON TEXT column.
//
//   - SQLite: json_extract(col, '$.key')
//   - PostgreSQL: (col::jsonb)->>'key'  (casts TEXT to JSONB for compatibility)
func (h dialectHelper) JSONExtract(col, key string) string {
	if h.IsPostgres() {
		return fmt.Sprintf("(%s::jsonb)->>'%s'", col, key)
	}
	return fmt.Sprintf("json_extract(%s, '$.%s')", col, key)
}

// AvgProcessingTimeMS returns an expression that computes the average duration in milliseconds
// between two timestamp columns.
//
//   - SQLite: AVG((julianday(end) - julianday(start)) * 24 * 60 * 60 * 1000)
//   - PostgreSQL: AVG(EXTRACT(EPOCH FROM (end - start)) * 1000)
func (h dialectHelper) AvgProcessingTimeMS(startCol, endCol string) string {
	if h.IsPostgres() {
		return fmt.Sprintf("EXTRACT(EPOCH FROM (%s - %s)) * 1000", endCol, startCol)
	}
	return fmt.Sprintf("(julianday(%s) - julianday(%s)) * 24 * 60 * 60 * 1000", endCol, startCol)
}

// q rewrites a SQL query for the active dialect.
//
// For PostgreSQL it:
//   - Replaces datetime('now') with NOW()
//   - Replaces date('now') with CURRENT_DATE
//   - Converts ? positional placeholders to $1, $2, … (skipping content inside
//     single-quoted string literals)
//
// For SQLite it returns the query unchanged.
func (h dialectHelper) q(query string) string {
	if !h.IsPostgres() {
		return query
	}
	query = strings.ReplaceAll(query, "datetime('now')", "NOW()")
	query = strings.ReplaceAll(query, "date('now')", "CURRENT_DATE")
	return rewritePostgresPlaceholders(query)
}

// rewritePostgresPlaceholders converts ? placeholders to $1, $2, … while
// correctly skipping content inside SQL single-quoted string literals.
func rewritePostgresPlaceholders(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 32)
	n := 1
	inStr := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case c == '\'' && inStr && i+1 < len(query) && query[i+1] == '\'':
			// Escaped single quote inside string literal — emit both and advance.
			b.WriteByte(c)
			b.WriteByte(c)
			i++
		case c == '\'':
			inStr = !inStr
			b.WriteByte(c)
		case c == '?' && !inStr:
			fmt.Fprintf(&b, "$%d", n)
			n++
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// ─── Dialect-aware DB wrappers ───────────────────────────────────────────────

// dialectAwareDB wraps *sql.DB and automatically applies dialect query rewriting
// before every ExecContext / QueryContext / QueryRowContext call.
// It satisfies the DBQuerier interface.
type dialectAwareDB struct {
	db      *sql.DB
	dialect dialectHelper
}

func newDialectAwareDB(db *sql.DB, d Dialect) *dialectAwareDB {
	return &dialectAwareDB{db: db, dialect: dialectHelper{d: d}}
}

func (d *dialectAwareDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.db.ExecContext(ctx, d.dialect.q(query), args...)
}

func (d *dialectAwareDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.db.QueryContext(ctx, d.dialect.q(query), args...)
}

func (d *dialectAwareDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.db.QueryRowContext(ctx, d.dialect.q(query), args...)
}

// BeginTx starts a transaction and returns a dialect-aware wrapper.
func (d *dialectAwareDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*dialectAwareTx, error) {
	tx, err := d.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &dialectAwareTx{tx: tx, dialect: d.dialect}, nil
}

// dialectAwareTx wraps *sql.Tx and automatically applies dialect query rewriting.
// It satisfies the DBQuerier interface.
type dialectAwareTx struct {
	tx      *sql.Tx
	dialect dialectHelper
}

func (t *dialectAwareTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.dialect.q(query), args...)
}

func (t *dialectAwareTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, t.dialect.q(query), args...)
}

func (t *dialectAwareTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, t.dialect.q(query), args...)
}

// PrepareContext prepares a statement with dialect query rewriting applied.
func (t *dialectAwareTx) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return t.tx.PrepareContext(ctx, t.dialect.q(query))
}

// Rollback delegates to the underlying transaction.
func (t *dialectAwareTx) Rollback() error { return t.tx.Rollback() }

// Commit delegates to the underlying transaction.
func (t *dialectAwareTx) Commit() error { return t.tx.Commit() }
