package database

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertStremioQueueItem inserts a queue item with an explicit download_id and
// updated_at, which is what GetFailedStremioQueueItems filters and orders on.
func insertStremioQueueItem(t *testing.T, db *sql.DB, nzbPath, status, downloadID, updatedAt string) {
	t.Helper()

	var dl any
	if downloadID != "" {
		dl = downloadID
	}

	_, err := db.Exec(
		`INSERT INTO import_queue (nzb_path, status, download_id, updated_at) VALUES (?, ?, ?, ?)`,
		nzbPath, status, dl, updatedAt,
	)
	require.NoError(t, err, "insert queue item %q", nzbPath)
}

func TestGetFailedStremioQueueItems(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:test_failed_stremio?mode=memory&cache=shared")
	require.NoError(t, err)
	defer db.Close()

	setupQueueSchema(t, db)

	// Two failed Stremio releases, plus rows that must NOT be returned.
	insertStremioQueueItem(t, db, "/nzbs/Older.Failed.nzb", "failed", "stremio:aaa", "2026-08-01 10:00:00")
	insertStremioQueueItem(t, db, "/nzbs/Newer.Failed.nzb", "failed", "stremio:bbb", "2026-08-02 10:00:00")
	insertStremioQueueItem(t, db, "/nzbs/Completed.nzb", "completed", "stremio:ccc", "2026-08-02 11:00:00")
	insertStremioQueueItem(t, db, "/nzbs/Pending.nzb", "pending", "stremio:ddd", "2026-08-02 11:00:00")
	insertStremioQueueItem(t, db, "/nzbs/Sab.Failed.nzb", "failed", "sab:eee", "2026-08-02 11:00:00")
	insertStremioQueueItem(t, db, "/nzbs/NoDownloadID.Failed.nzb", "failed", "", "2026-08-02 11:00:00")

	repo := NewRepository(db, DialectSQLite)

	items, err := repo.GetFailedStremioQueueItems(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 2, "only failed stremio: items should be returned")

	// Newest first: the query orders by updated_at DESC because
	// UpdateQueueItemStatus does not set completed_at on failure.
	assert.Equal(t, "/nzbs/Newer.Failed.nzb", items[0].NzbPath)
	assert.Equal(t, "/nzbs/Older.Failed.nzb", items[1].NzbPath)

	for _, item := range items {
		require.NotNil(t, item.DownloadID)
		assert.Contains(t, *item.DownloadID, "stremio:")
	}
}

func TestGetFailedStremioQueueItems_Empty(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:test_failed_stremio_empty?mode=memory&cache=shared")
	require.NoError(t, err)
	defer db.Close()

	setupQueueSchema(t, db)
	insertStremioQueueItem(t, db, "/nzbs/Completed.nzb", "completed", "stremio:aaa", "2026-08-02 11:00:00")

	repo := NewRepository(db, DialectSQLite)

	items, err := repo.GetFailedStremioQueueItems(context.Background())
	require.NoError(t, err)
	assert.Empty(t, items, "no failed stremio items should yield an empty result, not an error")
}
