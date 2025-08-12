package database

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed queue_migrations/*.sql
var embedQueueMigrations embed.FS

// QueueDB wraps the queue database connection and provides access to queue operations
type QueueDB struct {
	conn       *sql.DB
	Repository *QueueRepository
}

// QueueConfig holds queue database configuration
type QueueConfig struct {
	DatabasePath string
}

// NewQueueDB creates a new queue database connection and runs migrations
func NewQueueDB(config QueueConfig) (*QueueDB, error) {
	// Configure connection string optimized for write-heavy queue operations
	connString := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=-32000&_temp_store=MEMORY&_busy_timeout=30000", 
		config.DatabasePath)
	
	conn, err := sql.Open("sqlite3", connString)
	if err != nil {
		return nil, fmt.Errorf("failed to open queue database: %w", err)
	}

	// Set connection pool settings optimized for queue operations
	conn.SetMaxOpenConns(8)  // Fewer connections for queue operations
	conn.SetMaxIdleConns(3)  // Keep fewer idle connections
	conn.SetConnMaxLifetime(0)
	conn.SetConnMaxIdleTime(15 * time.Minute) // Shorter idle time

	// Test the connection
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping queue database: %w", err)
	}

	// Set SQLite pragmas optimized for write-heavy queue operations
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",           // WAL mode for concurrency
		"PRAGMA synchronous = NORMAL",         // Good balance for queue operations
		"PRAGMA cache_size = -32000",          // 32MB cache (smaller than main DB)
		"PRAGMA temp_store = MEMORY",          // Memory temp storage
		"PRAGMA busy_timeout = 30000",         // 30 second timeout
		"PRAGMA wal_autocheckpoint = 500",     // More frequent checkpoints for writes
		"PRAGMA optimize",                     // Optimize query planner
		"PRAGMA mmap_size = 268435456",        // 256MB memory map
	}

	for _, pragma := range pragmas {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to set queue pragma '%s': %w", pragma, err)
		}
	}

	// Run queue-specific migrations
	if err := runQueueMigrations(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run queue migrations: %w", err)
	}

	db := &QueueDB{
		conn: conn,
	}

	db.Repository = NewQueueRepository(conn)

	return db, nil
}

// runQueueMigrations runs queue database migrations in order
func runQueueMigrations(db *sql.DB) error {
	// Create migrations table if it doesn't exist
	createMigrationsTable := `
		CREATE TABLE IF NOT EXISTS queue_schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`

	if _, err := db.Exec(createMigrationsTable); err != nil {
		return fmt.Errorf("failed to create queue migrations table: %w", err)
	}

	// Get list of migration files
	entries, err := fs.ReadDir(embedQueueMigrations, "queue_migrations")
	if err != nil {
		return fmt.Errorf("failed to read queue migrations directory: %w", err)
	}

	// Sort migration files by name
	var migrationFiles []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		migrationFiles = append(migrationFiles, entry.Name())
	}
	sort.Strings(migrationFiles)

	// Apply migrations
	for _, filename := range migrationFiles {
		version := strings.TrimSuffix(filename, ".sql")

		// Check if migration is already applied
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM queue_schema_migrations WHERE version = ?", version).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check queue migration status: %w", err)
		}

		if count > 0 {
			continue // Migration already applied
		}

		// Read migration file
		migrationPath := filepath.Join("queue_migrations", filename)
		content, err := embedQueueMigrations.ReadFile(migrationPath)
		if err != nil {
			return fmt.Errorf("failed to read queue migration file %s: %w", filename, err)
		}

		// Execute migration
		migrationSQL := string(content)
		migrationSQL = cleanMigrationSQL(migrationSQL)

		if _, err := db.Exec(migrationSQL); err != nil {
			return fmt.Errorf("failed to execute queue migration %s: %w", version, err)
		}

		// Record migration as applied
		if _, err := db.Exec("INSERT INTO queue_schema_migrations (version) VALUES (?)", version); err != nil {
			return fmt.Errorf("failed to record queue migration %s: %w", version, err)
		}
	}

	return nil
}

// Close closes the queue database connection
func (db *QueueDB) Close() error {
	return db.conn.Close()
}

// Connection returns the underlying queue database connection
func (db *QueueDB) Connection() *sql.DB {
	return db.conn
}