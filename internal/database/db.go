package database

import (
	"database/sql"
	"embed"
	"fmt"
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
	Repository *QueueRepository
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

	if err := goose.Up(db, migrationsDir); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
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
