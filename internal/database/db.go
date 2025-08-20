package database

import (
	"database/sql"
	"embed"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"
)

//go:embed queue_migrations/*.sql
var embedQueueMigrations embed.FS

// DB wraps the database connection and provides access to operations
type DB struct {
	conn       *sql.DB
	Repository *QueueRepository
}

// Config holds database configuration
type Config struct {
	DatabasePath string
}

// NewDB creates a new database connection and runs migrations
func NewDB(config Config) (*DB, error) {
	// Configure connection string optimized for write-heavy queue operations
	connString := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=-32000&_temp_store=MEMORY&_busy_timeout=30000",
		config.DatabasePath)

	conn, err := sql.Open("sqlite3", connString)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings optimized for queue operations
	conn.SetMaxOpenConns(8) // Fewer connections for queue operations
	conn.SetMaxIdleConns(3) // Keep fewer idle connections
	conn.SetConnMaxLifetime(0)
	conn.SetConnMaxIdleTime(15 * time.Minute) // Shorter idle time

	// Test the connection
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Set SQLite pragmas optimized for write-heavy queue operations
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",       // WAL mode for concurrency
		"PRAGMA synchronous = NORMAL",     // Good balance for queue operations
		"PRAGMA cache_size = -32000",      // 32MB cache (smaller than main DB)
		"PRAGMA temp_store = MEMORY",      // Memory temp storage
		"PRAGMA busy_timeout = 30000",     // 30 second timeout
		"PRAGMA wal_autocheckpoint = 500", // More frequent checkpoints for writes
		"PRAGMA optimize",                 // Optimize query planner
		"PRAGMA mmap_size = 268435456",    // 256MB memory map
	}

	for _, pragma := range pragmas {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to set queue pragma '%s': %w", pragma, err)
		}
	}

	// Run database migrations
	if err := runMigrations(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	db := &DB{
		conn: conn,
	}

	db.Repository = NewQueueRepository(conn)

	return db, nil
}

// runMigrations runs database migrations using Goose
func runMigrations(db *sql.DB) error {
	// Set the migration provider for embedded filesystem
	goose.SetBaseFS(embedQueueMigrations)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.Up(db, "queue_migrations"); err != nil {
		return fmt.Errorf("failed to run queue migrations: %w", err)
	}

	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.conn.Close()
}

// Connection returns the underlying database connection
func (db *DB) Connection() *sql.DB {
	return db.conn
}
