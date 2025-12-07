package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *HealthRepository {
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	require.NoError(t, err)
	
	_, err = db.Exec(`
		CREATE TABLE file_health (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT NOT NULL UNIQUE,
			library_path TEXT,
			status TEXT NOT NULL,
			last_checked DATETIME,
			last_error TEXT,
			retry_count INTEGER DEFAULT 0,
			max_retries INTEGER DEFAULT 3,
			repair_retry_count INTEGER DEFAULT 0,
			max_repair_retries INTEGER DEFAULT 3,
			source_nzb_path TEXT,
			error_details TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			release_date DATETIME,
			scheduled_check_at DATETIME
		);
	`)
	require.NoError(t, err)

	return NewHealthRepository(db)
}

func TestGetFilesForRepairNotification_RespectsSchedule(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	// 1. Insert a file with repair_triggered status and future scheduled_check_at
	futureTime := time.Now().UTC().Add(1 * time.Hour)
	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (
			file_path, status, repair_retry_count, max_repair_retries, scheduled_check_at, last_checked
		) VALUES (?, ?, ?, ?, ?, ?)
	`, "future_repair.mkv", "repair_triggered", 0, 3, futureTime, time.Now().UTC())
	require.NoError(t, err)

	// 2. Insert a file with repair_triggered status and past scheduled_check_at
	pastTime := time.Now().UTC().Add(-1 * time.Hour)
	_, err = repo.db.ExecContext(ctx, `
		INSERT INTO file_health (
			file_path, status, repair_retry_count, max_repair_retries, scheduled_check_at, last_checked
		) VALUES (?, ?, ?, ?, ?, ?)
	`, "past_repair.mkv", "repair_triggered", 0, 3, pastTime, time.Now().UTC())
	require.NoError(t, err)

	// 3. Insert a file with repair_triggered status and NULL scheduled_check_at (should be picked up)
	_, err = repo.db.ExecContext(ctx, `
		INSERT INTO file_health (
			file_path, status, repair_retry_count, max_repair_retries, scheduled_check_at, last_checked
		) VALUES (?, ?, ?, ?, NULL, ?)
	`, "null_schedule_repair.mkv", "repair_triggered", 0, 3, time.Now().UTC())
	require.NoError(t, err)

	// Test GetFilesForRepairNotification
	files, err := repo.GetFilesForRepairNotification(ctx, 10)
	require.NoError(t, err)

	foundFuture := false
	foundPast := false
	foundNull := false

	for _, f := range files {
		if f.FilePath == "future_repair.mkv" {
			foundFuture = true
		}
		if f.FilePath == "past_repair.mkv" {
			foundPast = true
		}
		if f.FilePath == "null_schedule_repair.mkv" {
			foundNull = true
		}
	}

	assert.False(t, foundFuture, "Future scheduled repair should not be picked up")
	assert.True(t, foundPast, "Past scheduled repair should be picked up")
	assert.True(t, foundNull, "Null scheduled repair should be picked up")
}
