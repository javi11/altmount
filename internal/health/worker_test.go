package health

import (
	"context"
	"database/sql"
	"runtime"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/database"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupWorkerTestDB returns both a HealthRepository and the underlying *sql.DB so tests
// can query columns (like scheduled_check_at) that GetFileHealth doesn't expose.
func setupWorkerTestDB(t *testing.T) (*database.HealthRepository, *sql.DB) {
	t.Helper()

	db, err := sql.Open("sqlite3", "file::memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

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
			scheduled_check_at DATETIME,
			priority INTEGER DEFAULT 0,
			streaming_failure_count INTEGER DEFAULT 0,
			is_masked BOOLEAN DEFAULT FALSE
		);
	`)
	require.NoError(t, err)

	return database.NewHealthRepository(db, database.DialectSQLite), db
}

// queryScheduledAt reads the scheduled_check_at column directly for a given file path.
// go-sqlite3 may return datetimes in either space-separated or RFC3339 format.
func queryScheduledAt(t *testing.T, db *sql.DB, filePath string) *time.Time {
	t.Helper()
	var raw sql.NullString
	err := db.QueryRowContext(context.Background(),
		"SELECT scheduled_check_at FROM file_health WHERE file_path = ?", filePath,
	).Scan(&raw)
	require.NoError(t, err)
	if !raw.Valid {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, e := time.Parse(layout, raw.String); e == nil {
			result := parsed.UTC()
			return &result
		}
	}
	t.Fatalf("cannot parse scheduled_check_at value %q", raw.String)
	return nil
}

// TestAddToHealthCheck_NewFile_ScheduledWithinFiveMinutes verifies Fix 1:
// a freshly imported file must be scheduled for health check within 5 minutes,
// not the 0–24h window used by library sync.
func TestAddToHealthCheck_NewFile_ScheduledWithinFiveMinutes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	repo, db := setupWorkerTestDB(t)
	worker := &HealthWorker{healthRepo: repo}
	ctx := context.Background()

	before := time.Now().UTC()
	nzbPath := "/nzbs/test.nzb"
	err := worker.AddToHealthCheck(ctx, "movies/new_movie.mkv", &nzbPath)
	require.NoError(t, err)

	health, err := repo.GetFileHealth(ctx, "movies/new_movie.mkv")
	require.NoError(t, err)
	require.NotNil(t, health)
	assert.Equal(t, database.HealthStatusPending, health.Status)

	scheduled := queryScheduledAt(t, db, "movies/new_movie.mkv")
	require.NotNil(t, scheduled, "scheduled_check_at must be set for new files")

	maxAllowed := before.Add(5 * time.Minute).Add(2 * time.Second) // 2s tolerance for slow CI
	assert.True(t, !scheduled.Before(before.Add(-time.Second)),
		"scheduled time should not be in the past (scheduled=%v, before=%v)", scheduled, before)
	assert.True(t, scheduled.Before(maxAllowed),
		"new file should be scheduled within 5 minutes (scheduled=%v, max=%v)", scheduled, maxAllowed)
}

// TestAddToHealthCheck_NewFile_NotScheduledInDistantFuture confirms the scheduled time
// is not in the distant future that the old library-sync jitter (up to 24h) would produce.
func TestAddToHealthCheck_NewFile_NotScheduledInDistantFuture(t *testing.T) {
	repo, db := setupWorkerTestDB(t)
	worker := &HealthWorker{healthRepo: repo}
	ctx := context.Background()

	err := worker.AddToHealthCheck(ctx, "tv/show/ep01.mkv", nil)
	require.NoError(t, err)

	scheduled := queryScheduledAt(t, db, "tv/show/ep01.mkv")
	require.NotNil(t, scheduled)

	// Old bug: jitter up to 1440 minutes. New file should never be 1 hour away.
	oneHourFromNow := time.Now().UTC().Add(time.Hour)
	assert.True(t, scheduled.Before(oneHourFromNow),
		"scheduled time should be far less than 1 hour away, got %v", scheduled)
}

// TestAddToHealthCheck_ExistingPendingFile_NoChange confirms that calling
// AddToHealthCheck on an already-pending file is a no-op.
func TestAddToHealthCheck_ExistingPendingFile_NoChange(t *testing.T) {
	repo, _ := setupWorkerTestDB(t)
	worker := &HealthWorker{healthRepo: repo}
	ctx := context.Background()

	// Pre-insert a pending file
	err := repo.UpdateFileHealth(ctx, "movies/existing.mkv", database.HealthStatusPending, nil, nil, nil, false)
	require.NoError(t, err)

	healthBefore, err := repo.GetFileHealth(ctx, "movies/existing.mkv")
	require.NoError(t, err)

	// AddToHealthCheck on already-pending file should succeed without errors
	err = worker.AddToHealthCheck(ctx, "movies/existing.mkv", nil)
	require.NoError(t, err)

	healthAfter, err := repo.GetFileHealth(ctx, "movies/existing.mkv")
	require.NoError(t, err)
	require.NotNil(t, healthAfter)

	assert.Equal(t, database.HealthStatusPending, healthAfter.Status)
	assert.Equal(t, healthBefore.ID, healthAfter.ID)
}
