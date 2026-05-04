package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var embedMigrations embed.FS

// DB wraps the database connection and provides access to operations.
type DB struct {
	conn    *sql.DB
	dialect dialectHelper
	// Repository is kept for backwards-compat; prefer using Connection() directly.
	Repository    *QueueRepository
	MigrationRepo *ImportMigrationRepository
}

// Config holds database configuration.
type Config struct {
	// Type selects the backend: "sqlite" (default) or "postgres".
	Type         string
	DatabasePath string // SQLite only
	DSN          string // PostgreSQL only
}

// NewDB creates a new database connection and runs migrations.
func NewDB(config Config) (*DB, error) {
	switch config.Type {
	case "postgres":
		return newPostgresDB(config)
	default:
		return newSQLiteDB(config)
	}
}

// newSQLiteDB opens a SQLite database with queue-optimized settings.
func newSQLiteDB(config Config) (*DB, error) {
	connString := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=-32000&_temp_store=MEMORY&_busy_timeout=30000",
		config.DatabasePath)

	conn, err := sql.Open("sqlite3", connString)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	conn.SetMaxOpenConns(8)
	conn.SetMaxIdleConns(3)
	conn.SetConnMaxLifetime(0)
	conn.SetConnMaxIdleTime(15 * time.Minute)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = -32000",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA busy_timeout = 30000",
		"PRAGMA wal_autocheckpoint = 500",
		"PRAGMA optimize",
		"PRAGMA mmap_size = 268435456",
	}
	for _, pragma := range pragmas {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to set pragma '%s': %w", pragma, err)
		}
	}

	if err := runMigrations(conn, DialectSQLite); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	dh := dialectHelper{d: DialectSQLite}
	db := &DB{conn: conn, dialect: dh}
	db.Repository = NewQueueRepository(conn, DialectSQLite)
	db.MigrationRepo = NewImportMigrationRepository(conn, DialectSQLite)
	return db, nil
}

// newPostgresDB opens a PostgreSQL database and runs migrations.
func newPostgresDB(config Config) (*DB, error) {
	conn, err := sql.Open("pgx", config.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres database: %w", err)
	}

	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(5 * time.Minute)
	conn.SetConnMaxIdleTime(1 * time.Minute)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping postgres database: %w", err)
	}

	if err := runMigrations(conn, DialectPostgres); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run postgres migrations: %w", err)
	}

	dh := dialectHelper{d: DialectPostgres}
	db := &DB{conn: conn, dialect: dh}
	db.Repository = NewQueueRepository(conn, DialectPostgres)
	db.MigrationRepo = NewImportMigrationRepository(conn, DialectPostgres)
	return db, nil
}

// runMigrations runs goose migrations for the given dialect.
func runMigrations(db *sql.DB, d Dialect) error {
	goose.SetBaseFS(embedMigrations)

	var gooseDialect, migrationsDir string
	if d == DialectPostgres {
		gooseDialect = "postgres"
		migrationsDir = "migrations/postgres"
	} else {
		gooseDialect = "sqlite3"
		migrationsDir = "migrations/sqlite"
	}

	if err := goose.SetDialect(gooseDialect); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	fixDevBranchMigrationConflict(db, d)

	if err := goose.Up(db, migrationsDir); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// fixDevBranchMigrationConflict fixes an issue for users who applied the metadata migration as version 26
// before it was renamed to 27, causing a conflict with the perf_indexes migration.
func fixDevBranchMigrationConflict(db *sql.DB, d Dialect) {
	var tableExists bool
	if d == DialectPostgres {
		db.QueryRow("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'goose_db_version')").Scan(&tableExists)
	} else {
		var exists int
		db.QueryRow("SELECT 1 FROM sqlite_master WHERE type='table' AND name='goose_db_version'").Scan(&exists)
		tableExists = exists == 1
	}

	if !tableExists {
		return
	}

	var has26, has27 bool
	if d == DialectPostgres {
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM goose_db_version WHERE version_id = 26 AND is_applied = true)").Scan(&has26)
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM goose_db_version WHERE version_id = 27 AND is_applied = true)").Scan(&has27)
	} else {
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM goose_db_version WHERE version_id = 26 AND is_applied = 1)").Scan(&has26)
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM goose_db_version WHERE version_id = 27 AND is_applied = 1)").Scan(&has27)
	}

	if has26 && !has27 {
		hasMetadata := false
		if d == DialectPostgres {
			db.QueryRow("SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='file_health' AND column_name='metadata')").Scan(&hasMetadata)
		} else {
			rows, err := db.Query("PRAGMA table_info(file_health)")
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var cid int
					var name, ctype string
					var notnull int
					var dfltValue *string
					var pk int
					if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err == nil {
						if name == "metadata" {
							hasMetadata = true
							break
						}
					}
				}
			}
		}

		if hasMetadata {
			db.Exec("CREATE INDEX IF NOT EXISTS idx_queue_status_updated ON import_queue(status, updated_at)")
			if d == DialectPostgres {
				db.Exec("INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES (27, true, CURRENT_TIMESTAMP)")
			} else {
				db.Exec("INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES (27, 1, CURRENT_TIMESTAMP)")
			}
		}
	}
}

// Dialect returns the dialect helper for this database.
func (db *DB) Dialect() Dialect {
	return db.dialect.d
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// Connection returns the underlying database connection.
func (db *DB) Connection() *sql.DB {
	return db.conn
}

// StartCheckpointLoop starts a background goroutine that periodically forces a WAL checkpoint
// (SQLite only). Call once after opening the DB; stops when ctx is cancelled.
func (db *DB) StartCheckpointLoop(ctx context.Context, interval time.Duration) {
	if db.dialect.d != DialectSQLite {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var busy, log, checkpointed int
				row := db.conn.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
				if err := row.Scan(&busy, &log, &checkpointed); err != nil {
					slog.WarnContext(ctx, "WAL checkpoint failed", "err", err)
				} else {
					slog.DebugContext(ctx, "WAL checkpoint complete", "busy", busy, "log", log, "checkpointed", checkpointed)
				}
			}
		}
	}()
}

// UpdateConnectionPool adjusts the database connection pool settings based on worker count.
func (db *DB) UpdateConnectionPool(workerCount int) {
	if workerCount <= 0 {
		workerCount = 2
	}
	maxConns := workerCount + 4
	idleConns := max(workerCount/2, 2)
	db.conn.SetMaxOpenConns(maxConns)
	db.conn.SetMaxIdleConns(idleConns)
}
