package database

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Removing a completed import from AltMount's queue must also drop its
// import_history copy. The SABnzbd history view is a UNION whose history arm is
// suppressed by an anti-join only while the live completed queue row exists, so
// deleting just the queue row resurrects the job as a fresh "Completed" slot
// pointing at a path the ARR already imported and deleted — which makes Radarr
// re-run DownloadedMovieImportService and fail with "path does not exist"
// (issue #586).
func TestRemoveFromQueue_DropsHistoryCopy(t *testing.T) {
	repo, db := newSABHistoryRepo(t)
	ctx := context.Background()
	base := time.Now().UTC()

	insertQ(t, db, 5, "completed", "movies", base, false)
	insertH(t, db, 100, 5, "movies", base)

	// Deduped to a single slot while both rows exist.
	total, err := repo.CountSABnzbdHistory(ctx, "")
	require.NoError(t, err)
	require.Equal(t, 1, total)

	require.NoError(t, repo.RemoveFromQueue(ctx, 5))

	total, err = repo.CountSABnzbdHistory(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, 0, total, "history copy must not resurface after the queue row is deleted")

	rows, err := repo.ListSABnzbdHistory(ctx, "", 100, 0)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

// The same must hold for the bulk delete used by the web UI's multi-select.
func TestRemoveFromQueueBulk_DropsHistoryCopies(t *testing.T) {
	repo, db := newSABHistoryRepo(t)
	ctx := context.Background()
	base := time.Now().UTC()

	insertQ(t, db, 5, "completed", "movies", base, false)
	insertH(t, db, 100, 5, "movies", base)
	insertQ(t, db, 6, "completed", "movies", base, false)
	insertH(t, db, 101, 6, "movies", base)
	// An unrelated import that is not being deleted must survive untouched.
	insertQ(t, db, 7, "completed", "tv", base, false)
	insertH(t, db, 102, 7, "tv", base)

	res, err := repo.RemoveFromQueueBulk(ctx, []int64{5, 6})
	require.NoError(t, err)
	require.Equal(t, 2, res.DeletedCount)

	rows, err := repo.ListSABnzbdHistory(ctx, "", 100, 0)
	require.NoError(t, err)
	require.Len(t, rows, 1, "only the untouched import may remain")
	assert.Equal(t, int64(7), rows[0].ID)
}

// ...and for "Clear completed", which wipes the whole completed queue at once.
func TestClearCompletedQueueItems_DropsHistoryCopies(t *testing.T) {
	repo, db := newSABHistoryRepo(t)
	ctx := context.Background()
	base := time.Now().UTC()

	insertQ(t, db, 5, "completed", "movies", base, false)
	insertH(t, db, 100, 5, "movies", base)
	// A failed item and its (nonexistent) history must be left alone.
	insertQ(t, db, 6, "failed", "movies", base, false)

	_, count, err := repo.ClearCompletedQueueItems(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	rows, err := repo.ListSABnzbdHistory(ctx, "", 100, 0)
	require.NoError(t, err)
	require.Len(t, rows, 1, "only the failed queue item may remain")
	assert.Equal(t, int64(6), rows[0].ID)
	assert.Equal(t, "failed_queue", rows[0].Source)
}

// A history row that was never linked to this queue id must not be collateral
// damage: import_history rows with a NULL nzb_id are long-term history for jobs
// whose queue rows aged out and belong to no live queue item.
func TestRemoveFromQueue_LeavesUnlinkedHistoryAlone(t *testing.T) {
	repo, db := newSABHistoryRepo(t)
	ctx := context.Background()
	base := time.Now().UTC()

	insertQ(t, db, 5, "completed", "movies", base, false)
	insertH(t, db, 100, 5, "movies", base)
	insertH(t, db, 101, 0, "movies", base) // NULL nzb_id — unrelated history

	require.NoError(t, repo.RemoveFromQueue(ctx, 5))

	rows, err := repo.ListSABnzbdHistory(ctx, "", 100, 0)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(101), rows[0].ID)
}
