package database

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

// DB wraps the database connection and provides access to repositories
type DB struct {
	conn       *sql.DB
	Repository *Repository
}

// Config holds database configuration
type Config struct {
	DatabasePath string
}

// New creates a new database connection and runs migrations
func New(config Config) (*DB, error) {
	conn, err := sql.Open("sqlite3", config.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Set SQLite pragmas for better performance
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = 10000",
		"PRAGMA temp_store = MEMORY",
	}

	for _, pragma := range pragmas {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to set pragma '%s': %w", pragma, err)
		}
	}

	// Run migrations
	if err := runMigrations(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	db := &DB{
		conn: conn,
	}

	db.Repository = NewRepository(conn)

	// create root directory
	if err := db.createRootDirectory(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create root directory: %w", err)
	}

	return db, nil
}

// runMigrations runs database migrations in order
func runMigrations(db *sql.DB) error {
	// Create migrations table if it doesn't exist
	createMigrationsTable := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`

	if _, err := db.Exec(createMigrationsTable); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get list of migration files
	entries, err := fs.ReadDir(embedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
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
		err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}

		if count > 0 {
			continue // Migration already applied
		}

		// Read migration file
		migrationPath := filepath.Join("migrations", filename)
		content, err := embedMigrations.ReadFile(migrationPath)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", filename, err)
		}

		// Execute migration (simplified - ignores goose annotations)
		migrationSQL := string(content)
		migrationSQL = cleanMigrationSQL(migrationSQL)

		if _, err := db.Exec(migrationSQL); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", version, err)
		}

		// Record migration as applied
		if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
			return fmt.Errorf("failed to record migration %s: %w", version, err)
		}
	}

	return nil
}

// cleanMigrationSQL removes goose annotations from SQL
func cleanMigrationSQL(sql string) string {
	lines := strings.Split(sql, "\n")
	var cleanLines []string

	inUpSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "-- +goose Up") {
			inUpSection = true
			continue
		}
		if strings.HasPrefix(trimmed, "-- +goose Down") {
			break // Only process Up section
		}
		if strings.HasPrefix(trimmed, "-- +goose StatementBegin") ||
			strings.HasPrefix(trimmed, "-- +goose StatementEnd") {
			continue
		}

		if inUpSection {
			cleanLines = append(cleanLines, line)
		}
	}

	return strings.Join(cleanLines, "\n")
}

// createRootDirectory ensures the root directory exists in the database
func (db *DB) createRootDirectory() error {
	// Check if root directory already exists
	existing, err := db.Repository.GetVirtualFileByPath("/")
	if err != nil {
		return fmt.Errorf("failed to check root directory: %w", err)
	}

	if existing != nil {
		// Root directory already exists
		return nil
	}

	// Create root directory entry
	rootDir := &VirtualFile{
		NzbFileID:   nil, // NULL for system directories (no associated NZB)
		VirtualPath: "/",
		Filename:    "/",
		Size:        0,
		IsDirectory: true,
		ParentPath:  nil, // Root has no parent
	}

	if err := db.Repository.CreateVirtualFile(rootDir); err != nil {
		return fmt.Errorf("failed to create root directory: %w", err)
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
