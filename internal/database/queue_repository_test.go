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

func TestResetStaleItems_ResetsAllProcessingItems(t *testing.T) {
	// Setup
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err, "Failed to open in-memory database")
	defer db.Close()

	setupQueueSchema(t, db)

	// Insert items with different ages
	now := time.Now()

	// Item 1: Stuck for 15 minutes
	insertQueueItemWithTime(t, db, 1, "old.nzb", "processing", now.Add(-15*time.Minute))

	// Item 2: Stuck for 5 minutes
	insertQueueItemWithTime(t, db, 2, "recent.nzb", "processing", now.Add(-5*time.Minute))

	// Item 3: Already pending (should remain unchanged)
	insertQueueItemWithTime(t, db, 3, "pending.nzb", "pending", now)

	repo := NewQueueRepository(db)

	// Test: Reset stale items
	err = repo.ResetStaleItems(context.Background())
	require.NoError(t, err, "ResetStaleItems should not error")

	// Verify: Both processing items were reset
	status1 := getQueueItemStatus(t, db, 1)
	status2 := getQueueItemStatus(t, db, 2)
	status3 := getQueueItemStatus(t, db, 3)

	assert.Equal(t, "pending", status1, "Item 1 (15min old) should be reset to pending")
	assert.Equal(t, "pending", status2, "Item 2 (5min old) should be reset to pending")
	assert.Equal(t, "pending", status3, "Item 3 (already pending) should remain pending")

	// Verify: started_at was cleared for reset item
	var startedAt *time.Time
	err = db.QueryRow("SELECT started_at FROM import_queue WHERE id = 1").Scan(&startedAt)
	require.NoError(t, err)
	assert.Nil(t, startedAt, "started_at should be NULL after reset")
}

func TestResetStaleItems_NoItemsToReset(t *testing.T) {
	// Setup: Empty queue
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	setupQueueSchema(t, db)
	repo := NewQueueRepository(db)

	// Test: Reset with no items
	err = repo.ResetStaleItems(context.Background())
	require.NoError(t, err, "Should not error on empty queue")

	// Verify: No items in queue
	count := countQueueItemsByStatus(t, db, "pending")
	assert.Equal(t, 0, count, "Queue should still be empty")
}


func TestResetStaleItems_MixedStatuses(t *testing.T) {
	// Setup: Items with various statuses
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	setupQueueSchema(t, db)

	now := time.Now()

	// Insert items with different statuses
	insertQueueItemWithTime(t, db, 1, "old-processing.nzb", "processing", now.Add(-20*time.Minute))
	insertQueueItemWithTime(t, db, 2, "old-completed.nzb", "completed", now.Add(-20*time.Minute))
	insertQueueItemWithTime(t, db, 3, "old-failed.nzb", "failed", now.Add(-20*time.Minute))
	insertQueueItemWithTime(t, db, 4, "old-pending.nzb", "pending", now.Add(-20*time.Minute))

	repo := NewQueueRepository(db)

	// Test: Reset stale items
	err = repo.ResetStaleItems(context.Background())
	require.NoError(t, err)

	// Verify: Only processing items are affected
	status1 := getQueueItemStatus(t, db, 1)
	status2 := getQueueItemStatus(t, db, 2)
	status3 := getQueueItemStatus(t, db, 3)
	status4 := getQueueItemStatus(t, db, 4)

	assert.Equal(t, "pending", status1, "Old processing item should be reset")
	assert.Equal(t, "completed", status2, "Completed items should not be affected")
	assert.Equal(t, "failed", status3, "Failed items should not be affected")
	assert.Equal(t, "pending", status4, "Already pending items should remain pending")
}

func TestResetStaleItems_VeryOldItems(t *testing.T) {
	// Setup: Items stuck for hours/days
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	setupQueueSchema(t, db)

	now := time.Now()

	// Insert items with extreme ages
	insertQueueItemWithTime(t, db, 1, "1hour-old.nzb", "processing", now.Add(-1*time.Hour))
	insertQueueItemWithTime(t, db, 2, "1day-old.nzb", "processing", now.Add(-24*time.Hour))
	insertQueueItemWithTime(t, db, 3, "1week-old.nzb", "processing", now.Add(-7*24*time.Hour))

	repo := NewQueueRepository(db)

	// Test: Reset stale items
	err = repo.ResetStaleItems(context.Background())
	require.NoError(t, err)

	// Verify: All very old items were reset
	status1 := getQueueItemStatus(t, db, 1)
	status2 := getQueueItemStatus(t, db, 2)
	status3 := getQueueItemStatus(t, db, 3)

	assert.Equal(t, "pending", status1, "1 hour old item should be reset")
	assert.Equal(t, "pending", status2, "1 day old item should be reset")
	assert.Equal(t, "pending", status3, "1 week old item should be reset")

	// Verify: All reset items now pending
	pendingCount := countQueueItemsByStatus(t, db, "pending")
	assert.Equal(t, 3, pendingCount, "All old items should now be pending")

	processingCount := countQueueItemsByStatus(t, db, "processing")
	assert.Equal(t, 0, processingCount, "No items should remain in processing")
}

func TestResetStaleItems_UpdatedAtFieldUpdated(t *testing.T) {
	// Setup
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	setupQueueSchema(t, db)

	now := time.Now()
	insertQueueItemWithTime(t, db, 1, "old.nzb", "processing", now.Add(-20*time.Minute))

	// Get original updated_at
	var originalUpdatedAt time.Time
	err = db.QueryRow("SELECT updated_at FROM import_queue WHERE id = 1").Scan(&originalUpdatedAt)
	require.NoError(t, err)

	// Wait 1 second to ensure time difference (SQLite datetime has second precision)
	time.Sleep(1 * time.Second)

	repo := NewQueueRepository(db)

	// Test: Reset stale items
	err = repo.ResetStaleItems(context.Background())
	require.NoError(t, err)

	// Verify: updated_at was changed
	var newUpdatedAt time.Time
	err = db.QueryRow("SELECT updated_at FROM import_queue WHERE id = 1").Scan(&newUpdatedAt)
	require.NoError(t, err)

	assert.True(t, newUpdatedAt.After(originalUpdatedAt),
		"updated_at should be updated when item is reset")
}
