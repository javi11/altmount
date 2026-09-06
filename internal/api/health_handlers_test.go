package api

import (
	"bytes"
	"context"
	"database/sql"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/health"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// newHealthHandlerTestServer builds a minimal Server + fiber app wired only
// with the dependencies handleDirectHealthCheck touches before it would need
// a running health worker or a metadata reader: a real (in-memory sqlite)
// health repo and a HealthWorker that is deliberately never Start()'d. With
// the worker not running, PerformBackgroundCheck returns an error before
// spawning its background goroutine, so no worker internals need to be
// exercised here (see PerformBackgroundCheck in internal/health/worker.go).
func newHealthHandlerTestServer(t *testing.T) (*fiber.App, *database.HealthRepository) {
	t.Helper()

	db, err := sql.Open("sqlite3", "file::memory:?cache=shared&mode=memory")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS file_health (
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
			metadata TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			release_date DATETIME,
			scheduled_check_at DATETIME,
			priority INTEGER NOT NULL DEFAULT 0,
			streaming_failure_count INTEGER DEFAULT 0,
			is_masked BOOLEAN DEFAULT FALSE,
			indexer TEXT DEFAULT NULL,
			download_id TEXT DEFAULT NULL
		);
	`)
	require.NoError(t, err)

	healthRepo := database.NewHealthRepository(db, database.DialectSQLite)
	healthWorker := health.NewHealthWorker(nil, healthRepo, nil, nil, nil, nil, nil)

	server := &Server{
		healthRepo:   healthRepo,
		healthWorker: healthWorker,
	}

	app := fiber.New()
	app.Post("/api/health/:id/check-now", server.handleDirectHealthCheck)
	return app, healthRepo
}

func insertPendingHealthRecord(t *testing.T, repo *database.HealthRepository, filePath string) int64 {
	t.Helper()
	require.NoError(t, repo.UpdateFileHealthScheduled(context.Background(),
		filePath, database.HealthStatusPending, nil, nil, nil, false, time.Now().UTC()))

	fh, err := repo.GetFileHealth(context.Background(), filePath)
	require.NoError(t, err)
	require.NotNil(t, fh)
	return fh.ID
}

// TestHandleDirectHealthCheck_MalformedBodyReturns400 is the regression test
// for issue 3: genuinely malformed JSON in the optional recheck body must be
// reported to the caller instead of being silently treated as "no override".
func TestHandleDirectHealthCheck_MalformedBodyReturns400(t *testing.T) {
	app, repo := newHealthHandlerTestServer(t)
	id := insertPendingHealthRecord(t, repo, "complete/movie.mkv")

	req := httptest.NewRequest("POST", "/api/health/"+strconv.FormatInt(id, 10)+"/check-now", bytes.NewReader([]byte("{not-json")))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

	// The row must not have been transitioned to 'checking': body validation
	// happens before SetFileCheckingByID specifically so a rejected request
	// never leaves the record stuck.
	fh, err := repo.GetFileHealth(context.Background(), "complete/movie.mkv")
	require.NoError(t, err)
	require.NotNil(t, fh)
	require.Equal(t, database.HealthStatusPending, fh.Status)
}

// TestHandleDirectHealthCheck_AbsentBodyStillWorks confirms the legitimate
// "no body" case is still tolerated: the request must proceed past body
// parsing (not 400) even though the worker isn't running here.
func TestHandleDirectHealthCheck_AbsentBodyStillWorks(t *testing.T) {
	app, repo := newHealthHandlerTestServer(t)
	id := insertPendingHealthRecord(t, repo, "complete/movie.mkv")

	req := httptest.NewRequest("POST", "/api/health/"+strconv.FormatInt(id, 10)+"/check-now", nil)

	resp, err := app.Test(req)
	require.NoError(t, err)
	require.NotEqual(t, fiber.StatusBadRequest, resp.StatusCode,
		"an absent body must not be rejected as malformed JSON")

	// The row must have been transitioned to 'checking' by the handler before
	// PerformBackgroundCheck was attempted, proving body handling did not
	// short-circuit the request.
	fh, err := repo.GetFileHealth(context.Background(), "complete/movie.mkv")
	require.NoError(t, err)
	require.NotNil(t, fh)
	require.Equal(t, database.HealthStatusChecking, fh.Status)
}
