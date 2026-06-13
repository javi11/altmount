package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/arrs/model"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *HealthRepository {
	db, err := sql.Open("sqlite3", "file::memory:")
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
			metadata TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			release_date DATETIME,
			scheduled_check_at DATETIME,
			priority INTEGER DEFAULT 0,
			streaming_failure_count INTEGER DEFAULT 0,
			is_masked BOOLEAN DEFAULT FALSE,
			indexer TEXT DEFAULT NULL
		);
	`)
	require.NoError(t, err)

	return NewHealthRepository(db, DialectSQLite)
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

func TestRegisterCorruptedFile_PlaybackFailureBehavior(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	filePath := "tv/Show/Season 01/Episode 01.mkv"
	errorMsg := "no NZB data available for file"

	// 1. Simulate RegisterCorruptedFile call (e.g. from streaming failure)
	err := repo.RegisterCorruptedFile(ctx, filePath, nil, errorMsg)
	require.NoError(t, err)

	// 2. Check the file state
	fileHealth, err := repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fileHealth)

	// Assert FIX behavior:
	// Status = 'pending'
	// Priority = HealthPriorityNext (2)
	// RetryCount = MaxRetries - 1 (so it triggers repair on next check)
	assert.Equal(t, HealthStatusPending, fileHealth.Status, "Status should be pending to trigger check/repair")
	assert.Equal(t, HealthPriorityNext, fileHealth.Priority, "Priority should be high/next")
	assert.Equal(t, fileHealth.MaxRetries-1, fileHealth.RetryCount, "RetryCount should equal MaxRetries-1 to trigger immediate repair on next check")

	// 3. Verify GetUnhealthyFiles picks it up
	unhealthyFiles, err := repo.GetUnhealthyFiles(ctx, 10, "NONE", "/tmp/lib", 3)

	require.NoError(t, err)

	found := false
	for _, f := range unhealthyFiles {
		if f.FilePath == filePath {
			found = true
			break
		}
	}
	assert.True(t, found, "File should be picked up by GetUnhealthyFiles")
}

func TestHealthRepository_DeleteHealthRecordsByPrefix(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	// Insert some files
	files := []string{
		"movies/Movie1/file1.mkv",
		"movies/Movie1/file2.mkv",
		"movies/Movie2/file.mkv",
		"tv/Show1/S01E01.mkv",
		"movies/Movie1", // Folder record (exact match)
	}

	for _, f := range files {
		err := repo.UpdateFileHealth(ctx, f, HealthStatusHealthy, nil, nil, nil, false)
		require.NoError(t, err)
	}

	// Delete by prefix "movies/Movie1"
	count, err := repo.DeleteHealthRecordsByPrefix(ctx, "movies/Movie1")
	require.NoError(t, err)
	assert.Equal(t, int64(3), count) // "movies/Movie1/file1.mkv", "movies/Movie1/file2.mkv", "movies/Movie1"

	// Verify others are still there
	h, err := repo.GetFileHealth(ctx, "movies/Movie2/file.mkv")
	require.NoError(t, err)
	assert.NotNil(t, h)

	h, err = repo.GetFileHealth(ctx, "tv/Show1/S01E01.mkv")
	require.NoError(t, err)
	assert.NotNil(t, h)

	// Verify deleted are gone
	h, err = repo.GetFileHealth(ctx, "movies/Movie1/file1.mkv")
	require.NoError(t, err)
	assert.Nil(t, h)

	h, err = repo.GetFileHealth(ctx, "movies/Movie1")
	require.NoError(t, err)
	assert.Nil(t, h)
}

// queryScheduledCheckAt reads scheduled_check_at directly from the DB for a given file path.
// This is needed because GetFileHealth does not select that column.
// go-sqlite3 may return datetimes in either space-separated or RFC3339 format.
func queryScheduledCheckAt(t *testing.T, repo *HealthRepository, filePath string) *time.Time {
	t.Helper()
	var raw sql.NullString
	err := repo.db.QueryRowContext(context.Background(),
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

// TestUpdateFileHealthScheduled_SetsExplicitScheduledAt verifies Fix 1:
// the new DB method stores the caller-supplied scheduled_check_at instead of datetime('now').
func TestUpdateFileHealthScheduled_SetsExplicitScheduledAt(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	filePath := "movies/test.mkv"
	// Choose a time ~3 minutes in the future (representative of the new short jitter).
	scheduledAt := time.Now().UTC().Add(3 * time.Minute).Truncate(time.Second)

	err := repo.UpdateFileHealthScheduled(ctx, filePath, HealthStatusPending, nil, nil, nil, false, scheduledAt)
	require.NoError(t, err)

	got := queryScheduledCheckAt(t, repo, filePath)
	require.NotNil(t, got, "scheduled_check_at should be set")

	// SQLite stores with second precision; allow 1s tolerance.
	diff := got.Sub(scheduledAt)
	if diff < 0 {
		diff = -diff
	}
	assert.LessOrEqual(t, diff, time.Second,
		"scheduled_check_at should match the provided time (got %v, want %v)", got, scheduledAt)
}

// TestUpdateFileHealthScheduled_UpdatesExistingRecord confirms the method works as an upsert.
func TestUpdateFileHealthScheduled_UpdatesExistingRecord(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	filePath := "tv/show/ep01.mkv"

	// Insert initial record using the standard method.
	err := repo.UpdateFileHealth(ctx, filePath, HealthStatusPending, nil, nil, nil, false)
	require.NoError(t, err)

	// Now update with a specific scheduled time.
	future := time.Now().UTC().Add(4 * time.Minute).Truncate(time.Second)
	err = repo.UpdateFileHealthScheduled(ctx, filePath, HealthStatusPending, nil, nil, nil, false, future)
	require.NoError(t, err)

	got := queryScheduledCheckAt(t, repo, filePath)
	require.NotNil(t, got, "scheduled_check_at should be set after upsert")

	diff := got.Sub(future)
	if diff < 0 {
		diff = -diff
	}
	assert.LessOrEqual(t, diff, time.Second,
		"scheduled_check_at should be updated to %v, got %v", future, got)
}

// TestRelinkFileByFilename_UpdatesAnyStatus verifies that relinking works for any status,
// especially including healthy files, to ensure they have a library path for future repairs.
func TestRelinkFileByFilename_UpdatesAnyStatus(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	fileName := "Inception (2010).mkv"
	oldPath := "complete/Inception (2010).mkv"
	newPath := "Movies/Inception (2010)/Inception (2010).mkv"
	libPath := "/mnt/library/Movies/Inception (2010)/Inception (2010).mkv"

	// 1. Insert a HEALTHY record with no library path (typical for a new download before sync)
	err := repo.UpdateFileHealth(ctx, oldPath, HealthStatusHealthy, nil, nil, nil, false)
	require.NoError(t, err)

	// 2. Perform Relink (Rename semantics — no new content)
	relinked, err := repo.RelinkFileByFilename(ctx, fileName, newPath, libPath, nil, false)
	require.NoError(t, err)
	assert.True(t, relinked, "Should have relinked the healthy record")

	// 3. Verify the record was updated
	h, err := repo.GetFileHealth(ctx, newPath)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, libPath, *h.LibraryPath)
	assert.Equal(t, HealthStatusHealthy, h.Status, "Status should remain healthy under rename semantics")

	// Verify old path is gone (since it was updated)
	oldH, err := repo.GetFileHealth(ctx, oldPath)
	require.NoError(t, err)
	assert.Nil(t, oldH)

	// 4. Verify it ALSO works for corrupted records (classic repair flow)
	corruptedFile := "Movies/Matrix.mkv"
	err = repo.UpdateFileHealth(ctx, corruptedFile, HealthStatusCorrupted, nil, nil, nil, false)
	require.NoError(t, err)

	relinked, err = repo.RelinkFileByFilename(ctx, "Matrix.mkv", corruptedFile, "/lib/Matrix.mkv", nil, false)
	require.NoError(t, err)
	assert.True(t, relinked)
}

// TestRelinkFileByFilename_RenamePreservesRepairState verifies that a Rename relink
// (revalidate=false) does not disturb repair_triggered state: status, counters, errors
// and schedule must survive a library reorganization.
func TestRelinkFileByFilename_RenamePreservesRepairState(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	filePath := "tv/Show.S01E01/Show.S01E01.mkv"
	lastErr := "missing 7 segments"
	future := time.Now().UTC().Add(30 * time.Minute).Format("2006-01-02 15:04:05")
	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, retry_count, repair_retry_count,
			max_repair_retries, last_error, scheduled_check_at)
		VALUES (?, 'repair_triggered', 2, 1, 3, ?, ?)
	`, filePath, lastErr, future)
	require.NoError(t, err)

	relinked, err := repo.RelinkFileByFilename(ctx, "Show.S01E01.mkv", filePath, "/lib/Show.S01E01.mkv", nil, false)
	require.NoError(t, err)
	require.True(t, relinked)

	h, err := repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, HealthStatusRepairTriggered, h.Status, "Rename must not reset repair status")
	assert.Equal(t, 2, h.RetryCount)
	assert.Equal(t, 1, h.RepairRetryCount)
	require.NotNil(t, h.LastError)
	assert.Equal(t, lastErr, *h.LastError)
}

// TestRelinkFileByFilename_DownloadRevalidatesRepairState verifies that a Download relink
// (revalidate=true — a re-downloaded copy was just imported) resets the record to pending
// for immediate re-validation while preserving repair_retry_count as the per-title repair
// budget. This is the exit path from the repair loop: without it, the repair worker
// destroys the fresh import on its next tick.
func TestRelinkFileByFilename_DownloadRevalidatesRepairState(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	filePath := "tv/Show.S01E02/Show.S01E02.mkv"
	future := time.Now().UTC().Add(30 * time.Minute).Format("2006-01-02 15:04:05")
	past := time.Now().UTC().Add(-5 * time.Minute).Format("2006-01-02 15:04:05")
	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, retry_count, repair_retry_count,
			max_repair_retries, last_error, error_details, scheduled_check_at, updated_at)
		VALUES (?, 'repair_triggered', 2, 1, 3, 'missing segments', 'details', ?, ?)
	`, filePath, future, past)
	require.NoError(t, err)

	before := time.Now().UTC()
	relinked, err := repo.RelinkFileByFilename(ctx, "Show.S01E02.mkv", filePath, "/lib/Show.S01E02.mkv", nil, true)
	require.NoError(t, err)
	require.True(t, relinked)

	h, err := repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, HealthStatusPending, h.Status, "Download must reset status so the new copy is validated")
	assert.Equal(t, 0, h.RetryCount, "retry_count restarts for the new copy")
	assert.Equal(t, 1, h.RepairRetryCount, "repair budget must be preserved so bad re-downloads still escalate")
	assert.Nil(t, h.LastError)
	assert.Nil(t, h.ErrorDetails)

	// scheduled_check_at must be pulled back from the future to now.
	var raw string
	err = repo.db.QueryRowContext(ctx,
		"SELECT scheduled_check_at FROM file_health WHERE file_path = ?", filePath).Scan(&raw)
	require.NoError(t, err)
	var scheduled time.Time
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, e := time.Parse(layout, raw); e == nil {
			scheduled = parsed.UTC()
			break
		}
	}
	assert.True(t, scheduled.Before(before.Add(time.Minute)),
		"scheduled_check_at should be ~now, got %v", scheduled)
}

// TestGetFilesForRepairNotification_ReturnsExhausted verifies that records whose repair
// budget is spent are still returned, so the worker can finalize them as corrupted.
// Filtering them out would leave them stuck in repair_triggered forever: no other
// query selects that status.
func TestGetFilesForRepairNotification_ReturnsExhausted(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	pastTime := time.Now().UTC().Add(-1 * time.Hour)
	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (
			file_path, status, repair_retry_count, max_repair_retries, scheduled_check_at, last_checked
		) VALUES (?, ?, ?, ?, ?, ?)
	`, "exhausted_repair.mkv", "repair_triggered", 3, 3, pastTime, time.Now().UTC())
	require.NoError(t, err)

	files, err := repo.GetFilesForRepairNotification(ctx, 10)
	require.NoError(t, err)

	found := false
	for _, f := range files {
		if f.FilePath == "exhausted_repair.mkv" {
			found = true
		}
	}
	assert.True(t, found, "exhausted repair records must be returned for finalization as corrupted")
}

// TestAddFileToHealthCheck_ConflictPreservesRepairBudget verifies that a re-import upsert
// over an existing record resets it to pending for re-validation but does NOT reset
// repair_retry_count: a re-download of a broken release must not refill its own repair
// budget, or genuinely dead releases would be re-downloaded forever.
func TestAddFileToHealthCheck_ConflictPreservesRepairBudget(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	filePath := "tv/Show.S01E03/Show.S01E03.mkv"
	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, retry_count, repair_retry_count, max_repair_retries)
		VALUES (?, 'repair_triggered', 2, 2, 3)
	`, filePath)
	require.NoError(t, err)

	err = repo.AddFileToHealthCheckWithMetadata(ctx, filePath, &filePath, 3, 3, nil, HealthPriorityNext, nil, nil, nil)
	require.NoError(t, err)

	h, err := repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, HealthStatusPending, h.Status)
	assert.Equal(t, 0, h.RetryCount, "retry_count resets for the new copy")
	assert.Equal(t, 2, h.RepairRetryCount, "repair budget must survive the re-import upsert")
}

// TestAddFileToHealthCheck_NormalizesLeadingSlash verifies the upsert strips a leading
// slash so import-side paths ("/tv/...") and webhook-side paths ("tv/...") cannot create
// duplicate rows for the same virtual file.
func TestAddFileToHealthCheck_NormalizesLeadingSlash(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	slashed := "/tv/Show.S01E04/Show.S01E04.mkv"
	err := repo.AddFileToHealthCheckWithMetadata(ctx, slashed, &slashed, 3, 3, nil, HealthPriorityNext, nil, nil, nil)
	require.NoError(t, err)

	h, err := repo.GetFileHealth(ctx, "tv/Show.S01E04/Show.S01E04.mkv")
	require.NoError(t, err)
	require.NotNil(t, h, "record must be stored without the leading slash")
}

// TestAddFileToHealthCheckWithMetadata_StoresLibraryPath verifies that new files
// added to the health check correctly store their library path.
func TestAddFileToHealthCheckWithMetadata_StoresLibraryPath(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	filePath := "Movies/Dune (2021)/Dune (2021).mkv"
	libraryPath := "/mnt/library/Movies/Dune (2021)/Dune (2021).mkv"
	sourceNzb := "Dune.nzb"

	// Add the file
	err := repo.AddFileToHealthCheckWithMetadata(ctx, filePath, &libraryPath, 3, 3, &sourceNzb, HealthPriorityNormal, nil, nil, nil)
	require.NoError(t, err)

	// Verify it was stored
	h, err := repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, h)
	require.NotNil(t, h.LibraryPath)
	assert.Equal(t, libraryPath, *h.LibraryPath)
	assert.Equal(t, filePath, h.FilePath)
}

func TestGetUnhealthyFiles_MatchesWindowsLibraryPath(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-1 * time.Hour)
	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, library_path, status, retry_count, max_retries, scheduled_check_at)
		VALUES ('tv/Show/Episode.mkv', 'C:\\rclone\\show-torrents\\Show\\Season 1\\Episode.mkv', 'pending', 0, 3, ?)
	`, past)
	require.NoError(t, err)

	files, err := repo.GetUnhealthyFiles(ctx, 10, "SYMLINK", `C:\rclone`, 3)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "tv/Show/Episode.mkv", files[0].FilePath)
}

func TestRelinkFileByMetadata(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	// 1. Set up a record with status repair_triggered and metadata JSON
	oldPath := "complete/tv/show.S01E01.mkv"
	libraryPath := "/mnt/library/tv/show.S01E01.mkv"
	dbMeta := `{"eventType":"Download","instanceName":"Sonarr","series":{"id":100,"tvdbId":200},"episodeFile":{"id":300},"episodes":[{"id":400}]}`

	err := repo.AddFileToHealthCheckWithMetadata(ctx, oldPath, &libraryPath, 3, 3, nil, HealthPriorityNormal, nil, &dbMeta, nil)
	require.NoError(t, err)

	// Update its status to repair_triggered
	err = repo.UpdateFileHealth(ctx, oldPath, HealthStatusRepairTriggered, nil, nil, nil, false)
	require.NoError(t, err)

	// Back-date updated_at so the 60-second guard doesn't block the reset to pending
	past := time.Now().UTC().Add(-5 * time.Minute).Format("2006-01-02 15:04:05")
	_, err = repo.db.ExecContext(ctx, "UPDATE file_health SET updated_at = ? WHERE file_path = ?", past, oldPath)
	require.NoError(t, err)

	// Verify it's repair_triggered
	hOld, err := repo.GetFileHealth(ctx, oldPath)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusRepairTriggered, hOld.Status)

	// 2. Perform Relink by Metadata
	newPath := "tv/show/Season 01/show.S01E01.mkv"
	newLibPath := "/mnt/library/tv/show/Season 01/show.S01E01.mkv"
	webMeta := model.WebhookMetadata{
		EventType:    "Download",
		InstanceName: "Sonarr",
		Series: &model.SeriesMetadata{
			Id:     100,
			TvdbId: 200,
		},
		Episodes: []model.EpisodeMetadata{
			{Id: 400},
		},
	}
	webMetaBytes, err := json.Marshal(webMeta)
	require.NoError(t, err)
	webMetaStr := string(webMetaBytes)

	relinked, err := repo.RelinkFileByMetadata(ctx, &webMeta, newPath, newLibPath, &webMetaStr, true)
	require.NoError(t, err)
	assert.True(t, relinked, "Should successfully relink by metadata IDs")

	// 3. Verify the old record got updated to the new paths and reset to pending
	hNew, err := repo.GetFileHealth(ctx, newPath)
	require.NoError(t, err)
	require.NotNil(t, hNew)
	assert.Equal(t, HealthStatusPending, hNew.Status)
	assert.Equal(t, newLibPath, *hNew.LibraryPath)
	assert.Equal(t, 0, hNew.RetryCount)
	assert.Equal(t, 0, hNew.RepairRetryCount)

	// Verify old path is gone
	oldH, err := repo.GetFileHealth(ctx, oldPath)
	require.NoError(t, err)
	assert.Nil(t, oldH)
}

func TestResetStalePendingFiles_ResetsStuckPending(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	filePath := "movies/stuck.mkv"
	futureTime := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)

	// Add a record scheduled in the distant future with status = 'pending' and retry_count = 0
	err := repo.UpdateFileHealthScheduled(ctx, filePath, HealthStatusPending, nil, nil, nil, false, futureTime)
	require.NoError(t, err)

	// Verify it has the future scheduled time
	hBefore, err := repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, hBefore)
	assert.Equal(t, HealthStatusPending, hBefore.Status)
	assert.Equal(t, 0, hBefore.RetryCount)
	
	// Now run ResetStalePendingFiles
	err = repo.ResetStalePendingFiles(ctx)
	require.NoError(t, err)

	// Verify scheduled_check_at was reset to the present/past
	hAfter, err := repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, hAfter)
	
	got := queryScheduledCheckAt(t, repo, filePath)
	require.NotNil(t, got)
	assert.True(t, got.Before(time.Now().UTC().Add(1*time.Minute)), "stuck file scheduled time should be reset to now (got %v)", got)
}

func TestRelinkFileByMetadata_ConflictMerge(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	// 1. Setup old record with metadata and repair history
	oldPath := "complete/tv/show.S01E01.mkv"
	libraryPath := "/mnt/library/tv/show.S01E01.mkv"
	dbMeta := `{"eventType":"Download","instanceName":"Sonarr","series":{"id":100,"tvdbId":200},"episodeFile":{"id":300},"episodes":[{"id":400}]}`
	past := time.Now().UTC().Add(-5 * time.Minute).Format("2006-01-02 15:04:05")
	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, repair_retry_count, max_repair_retries, metadata, library_path, updated_at)
		VALUES (?, 'repair_triggered', 2, 3, ?, ?, ?)
	`, oldPath, dbMeta, libraryPath, past)
	require.NoError(t, err)

	// 2. Setup conflicting record at the target path
	newPath := "tv/show/Season 01/show.S01E01.mkv"
	newLibPath := "/mnt/library/tv/show/Season 01/show.S01E01.mkv"
	_, err = repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, repair_retry_count, max_repair_retries, metadata, library_path, updated_at)
		VALUES (?, 'corrupted', 1, 3, ?, ?, ?)
	`, newPath, dbMeta, newLibPath, past)
	require.NoError(t, err)

	// 3. Relink
	webMeta := model.WebhookMetadata{
		EventType:    "Download",
		InstanceName: "Sonarr",
		Series: &model.SeriesMetadata{
			Id:     100,
			TvdbId: 200,
		},
		Episodes: []model.EpisodeMetadata{
			{Id: 400},
		},
	}
	webMetaBytes, err := json.Marshal(webMeta)
	require.NoError(t, err)
	webMetaStr := string(webMetaBytes)

	relinked, err := repo.RelinkFileByMetadata(ctx, &webMeta, newPath, newLibPath, &webMetaStr, true)
	require.NoError(t, err)
	assert.True(t, relinked)

	// 4. Verify conflicting/new record is merged and updated
	h, err := repo.GetFileHealth(ctx, newPath)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, HealthStatusPending, h.Status)
	assert.Equal(t, 2, h.RepairRetryCount, "Should carry over the maximum repair retry count (2)")
	assert.Equal(t, 0, h.RetryCount)

	// Verify old record is deleted
	oldH, err := repo.GetFileHealth(ctx, oldPath)
	require.NoError(t, err)
	assert.Nil(t, oldH)
}

func TestRelinkFileByFilename_ConflictMerge(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	fileName := "Matrix.mkv"
	oldPath := "complete/Matrix.mkv"
	newPath := "Movies/Matrix/Matrix.mkv"
	newLibPath := "/lib/Matrix.mkv"

	// 1. Setup old record with repair history
	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, repair_retry_count, max_repair_retries)
		VALUES (?, 'repair_triggered', 3, 3)
	`, oldPath)
	require.NoError(t, err)

	// 2. Setup conflicting record at the target path
	_, err = repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, repair_retry_count, max_repair_retries)
		VALUES (?, 'healthy', 1, 3)
	`, newPath)
	require.NoError(t, err)

	// 3. Relink
	relinked, err := repo.RelinkFileByFilename(ctx, fileName, newPath, newLibPath, nil, true)
	require.NoError(t, err)
	assert.True(t, relinked)

	// 4. Verify record at new path is updated and merged
	h, err := repo.GetFileHealth(ctx, newPath)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, HealthStatusPending, h.Status)
	assert.Equal(t, 3, h.RepairRetryCount, "Should carry over the maximum repair retry count (3)")
	assert.Equal(t, 0, h.RetryCount)

	// Verify old path is gone
	oldH, err := repo.GetFileHealth(ctx, oldPath)
	require.NoError(t, err)
	assert.Nil(t, oldH)
}

