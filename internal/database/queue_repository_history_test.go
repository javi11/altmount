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

// QueueRepository is the second, older type exposing RemoveFromQueue /
// RemoveFromQueueBulk (DB.Repository is one of these, and the importer calls
// through it). It must give the same "queue row and its history copy go
// together" guarantee as Repository, or the SABnzbd history view resurrects the
// job exactly as in issue #586.
func newQueueRepoWithHistory(t *testing.T) (*QueueRepository, *Repository, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	setupQueueSchema(t, db)
	setupImportHistorySchema(t, db)
	t.Cleanup(func() { _ = db.Close() })
	return NewQueueRepository(db, DialectSQLite), NewRepository(db, DialectSQLite), db
}

func TestQueueRepository_RemoveFromQueue_DropsHistoryCopy(t *testing.T) {
	qrepo, repo, db := newQueueRepoWithHistory(t)
	ctx := context.Background()
	base := time.Now().UTC()

	insertQ(t, db, 5, "completed", "movies", base, false)
	insertH(t, db, 100, 5, "movies", base)

	require.NoError(t, qrepo.RemoveFromQueue(ctx, 5))

	rows, err := repo.ListSABnzbdHistory(ctx, "", 100, 0)
	require.NoError(t, err)
	assert.Empty(t, rows, "history copy must not resurface after the queue row is deleted")
}

func TestQueueRepository_RemoveFromQueueBulk_DropsHistoryCopies(t *testing.T) {
	qrepo, repo, db := newQueueRepoWithHistory(t)
	ctx := context.Background()
	base := time.Now().UTC()

	insertQ(t, db, 5, "completed", "movies", base, false)
	insertH(t, db, 100, 5, "movies", base)
	insertQ(t, db, 6, "completed", "movies", base, false)
	insertH(t, db, 101, 6, "movies", base)
	// Untouched import must survive.
	insertQ(t, db, 7, "completed", "tv", base, false)
	insertH(t, db, 102, 7, "tv", base)

	res, err := qrepo.RemoveFromQueueBulk(ctx, []int64{5, 6})
	require.NoError(t, err)
	require.Equal(t, 2, res.DeletedCount)

	rows, err := repo.ListSABnzbdHistory(ctx, "", 100, 0)
	require.NoError(t, err)
	require.Len(t, rows, 1, "only the untouched import may remain")
	assert.Equal(t, int64(7), rows[0].ID)
}

// A processing item is refused, and its history copy must be left alone too.
func TestQueueRepository_RemoveFromQueueBulk_SkipsProcessingAndItsHistory(t *testing.T) {
	qrepo, repo, db := newQueueRepoWithHistory(t)
	ctx := context.Background()
	base := time.Now().UTC()

	insertQ(t, db, 8, "processing", "movies", base, false)
	insertH(t, db, 103, 8, "movies", base)

	res, _ := qrepo.RemoveFromQueueBulk(ctx, []int64{8})
	require.Equal(t, 0, res.DeletedCount)
	require.Equal(t, 1, res.ProcessingCount)

	rows, err := repo.ListSABnzbdHistory(ctx, "", 100, 0)
	require.NoError(t, err)
	assert.Len(t, rows, 1, "a refused delete must leave the history row in place")
}
