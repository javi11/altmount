package api

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/importer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSABnzbdDeleteTestServer(t *testing.T) (*Server, *database.Repository) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.NewDB(database.Config{Type: "sqlite", DatabasePath: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := database.NewRepository(db.Connection(), db.Dialect())

	server := &Server{
		queueRepo:       repo,
		importerService: &importer.Service{}, // non-nil sentinel, not used for delete handlers
	}

	return server, repo
}

func addFailedQueueItem(t *testing.T, repo *database.Repository, nzbPath string) int64 {
	t.Helper()

	require.NoError(t, os.WriteFile(nzbPath, []byte("fake nzb"), 0o644))

	item := &database.ImportQueueItem{
		NzbPath:  nzbPath,
		Status:   database.QueueStatusFailed,
		Priority: database.QueuePriorityNormal,
	}
	require.NoError(t, repo.AddToQueue(context.Background(), item))
	return item.ID
}

func TestHandleSABnzbdQueueDelete_RemovesFailedNzbFile(t *testing.T) {
	server, repo := newSABnzbdDeleteTestServer(t)

	nzbPath := filepath.Join(t.TempDir(), "failed.nzb")
	id := addFailedQueueItem(t, repo, nzbPath)
	require.NotZero(t, id)

	app := fiber.New()
	app.Delete("/sabnzbd", server.handleSABnzbdQueueDelete)

	req := httptest.NewRequest("DELETE", "/sabnzbd?value=1&name=queue", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	// Row must be gone from the queue.
	remaining, err := repo.GetQueueItem(context.Background(), id)
	require.NoError(t, err)
	assert.Nil(t, remaining, "queue row should be deleted")

	// The NZB file left in the failed directory must be removed too.
	_, statErr := os.Stat(nzbPath)
	assert.True(t, os.IsNotExist(statErr), "failed NZB file should have been deleted, got err=%v", statErr)
}

func TestHandleSABnzbdHistoryDelete_RemovesFailedNzbFile(t *testing.T) {
	server, repo := newSABnzbdDeleteTestServer(t)

	nzbPath := filepath.Join(t.TempDir(), "failed.nzb")
	id := addFailedQueueItem(t, repo, nzbPath)
	require.NotZero(t, id)

	app := fiber.New()
	app.Delete("/sabnzbd", server.handleSABnzbdHistoryDelete)

	req := httptest.NewRequest("DELETE", "/sabnzbd?value=1&name=delete", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	remaining, err := repo.GetQueueItem(context.Background(), id)
	require.NoError(t, err)
	assert.Nil(t, remaining, "queue row should be deleted")

	_, statErr := os.Stat(nzbPath)
	assert.True(t, os.IsNotExist(statErr), "failed NZB file should have been deleted, got err=%v", statErr)
}

func TestHandleSABnzbdQueueDelete_ByDownloadID_RemovesFailedNzbFile(t *testing.T) {
	server, repo := newSABnzbdDeleteTestServer(t)

	nzbPath := filepath.Join(t.TempDir(), "failed.nzb")
	require.NoError(t, os.WriteFile(nzbPath, []byte("fake nzb"), 0o644))

	downloadID := "sabnzbd-download-id"
	item := &database.ImportQueueItem{
		DownloadID: &downloadID,
		NzbPath:    nzbPath,
		Status:     database.QueueStatusFailed,
		Priority:   database.QueuePriorityNormal,
	}
	require.NoError(t, repo.AddToQueue(context.Background(), item))

	app := fiber.New()
	app.Delete("/sabnzbd", server.handleSABnzbdQueueDelete)

	req := httptest.NewRequest("DELETE", "/sabnzbd?value="+downloadID+"&name=queue", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	_, statErr := os.Stat(nzbPath)
	assert.True(t, os.IsNotExist(statErr), "failed NZB file should have been deleted, got err=%v", statErr)
}
