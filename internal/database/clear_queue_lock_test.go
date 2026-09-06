package database

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// clearQueueItemsByStatus lists the nzb_paths it is about to delete and then
// deletes, all inside one transaction. SQLite opens transactions in DEFERRED
// mode, so if that transaction's FIRST statement is the SELECT, the connection
// takes a read snapshot and only later tries to upgrade to a write. Under WAL,
// an upgrade whose snapshot another connection has already invalidated returns
// SQLITE_BUSY_SNAPSHOT, which busy_timeout deliberately does NOT retry — the
// clear fails outright with "database is locked".
//
// A "clear completed" fired while the importer is committing queue updates is
// exactly that race, so the transaction has to start with a write.
func TestClearCompletedQueueItems_SurvivesConcurrentWriter(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clear.db")
	db, err := sql.Open("sqlite3", fmt.Sprintf(
		"file:%s?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000", dbPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(4)

	setupQueueSchema(t, db)
	setupImportHistorySchema(t, db)

	repo := NewRepository(db, DialectSQLite)
	ctx := context.Background()

	// A row the concurrent writer keeps touching, so every one of its commits
	// invalidates any read snapshot taken before it.
	_, err = db.Exec(`INSERT INTO import_queue (id, nzb_path, status, priority) VALUES (1, 'busy.nzb', 'pending', 1)`)
	require.NoError(t, err)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Each UPDATE is its own committed write transaction.
			_, _ = db.Exec(`UPDATE import_queue SET updated_at = datetime('now') WHERE id = 1`)
		}
	}()
	t.Cleanup(func() {
		close(stop)
		wg.Wait()
	})

	base := time.Now().UTC()
	for i := range 200 {
		id := int64(1000 + i)
		insertQ(t, db, id, "completed", "movies", base, false)
		insertH(t, db, id, id, "movies", base)

		paths, deleted, err := repo.ClearCompletedQueueItems(ctx)
		if err != nil && strings.Contains(err.Error(), "locked") {
			t.Fatalf("iteration %d: clear failed under a concurrent writer: %v", i, err)
		}
		require.NoErrorf(t, err, "iteration %d", i)
		require.Equalf(t, 1, deleted, "iteration %d", i)
		require.Lenf(t, paths, 1, "iteration %d: the deleted row's path must reach the caller for on-disk cleanup", i)
	}
}
