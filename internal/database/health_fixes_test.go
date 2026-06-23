package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fix A: corrupted is terminal at the query level, and backfill never re-arms it ---

// TestGetUnhealthyFiles_ExcludesCorrupted verifies that a 'corrupted' record is never
// selected for checking even when it has a due scheduled_check_at and retry budget left,
// so no re-arm vector (e.g. an unconditional release-date backfill) can resurrect it.
func TestGetUnhealthyFiles_ExcludesCorrupted(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-1 * time.Hour)
	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, retry_count, max_retries, scheduled_check_at)
		VALUES ('corrupted.mkv', 'corrupted', 2, 3, ?), ('pending.mkv', 'pending', 0, 3, ?)
	`, past, past)
	require.NoError(t, err)

	files, err := repo.GetUnhealthyFiles(ctx, 10, "NONE", "", 3)
	require.NoError(t, err)

	paths := map[string]bool{}
	for _, f := range files {
		paths[f.FilePath] = true
	}
	assert.False(t, paths["corrupted.mkv"], "corrupted record must not be selected for checking")
	assert.True(t, paths["pending.mkv"], "pending record must still be selected (control)")
}

// TestBackfillReleaseDates_DoesNotRearmTerminal verifies the status-gated backfill:
// release_date is filled for every record, but scheduled_check_at is only (re)scheduled
// for non-terminal/non-repair records. A corrupted record's NULL schedule must survive.
func TestBackfillReleaseDates_DoesNotRearmTerminal(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, scheduled_check_at, release_date)
		VALUES ('dead.mkv', 'corrupted', NULL, NULL), ('alive.mkv', 'healthy', NULL, NULL)
	`)
	require.NoError(t, err)

	dead, err := repo.GetFileHealth(ctx, "dead.mkv")
	require.NoError(t, err)
	alive, err := repo.GetFileHealth(ctx, "alive.mkv")
	require.NoError(t, err)

	future := time.Now().UTC().Add(48 * time.Hour)
	relDate := time.Now().UTC().Add(-30 * 24 * time.Hour)
	err = repo.BackfillReleaseDates(ctx, []BackfillUpdate{
		{ID: dead.ID, ReleaseDate: relDate, ScheduledCheckAt: future},
		{ID: alive.ID, ReleaseDate: relDate, ScheduledCheckAt: future},
	})
	require.NoError(t, err)

	dead, err = repo.GetFileHealth(ctx, "dead.mkv")
	require.NoError(t, err)
	require.NotNil(t, dead.ReleaseDate, "release_date must be backfilled even for corrupted records")

	alive, err = repo.GetFileHealth(ctx, "alive.mkv")
	require.NoError(t, err)
	require.NotNil(t, alive.ReleaseDate)

	// GetFileHealth does not project scheduled_check_at, so read it directly.
	assert.False(t, scheduledCheckAt(t, repo, "dead.mkv").Valid,
		"backfill must NOT re-arm a corrupted record's terminal NULL schedule")
	assert.True(t, scheduledCheckAt(t, repo, "alive.mkv").Valid,
		"healthy record must be (re)scheduled by backfill")
}

// scheduledCheckAt reads the raw scheduled_check_at column (not projected by GetFileHealth).
func scheduledCheckAt(t *testing.T, repo *HealthRepository, filePath string) sql.NullString {
	t.Helper()
	var v sql.NullString
	require.NoError(t, repo.db.QueryRowContext(context.Background(),
		"SELECT scheduled_check_at FROM file_health WHERE file_path = ?", filePath).Scan(&v))
	return v
}

// --- Fix B: status-conditional bulk updates close the TOCTOU window ---

// TestUpdateHealthStatusBulk_ExpectedStatusGuard verifies that a guarded write only lands
// when the record still holds the expected status, so a concurrent transition (e.g. a
// webhook relink moving repair_triggered -> pending) is not clobbered.
func TestUpdateHealthStatusBulk_ExpectedStatusGuard(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, repair_retry_count, max_repair_retries)
		VALUES ('rescued.mkv', 'pending', 1, 3), ('still_repair.mkv', 'repair_triggered', 1, 3)
	`)
	require.NoError(t, err)

	repairStatus := HealthStatusRepairTriggered
	now := time.Now().UTC()
	err = repo.UpdateHealthStatusBulk(ctx, []HealthStatusUpdate{
		{Type: UpdateTypeRepairRetry, FilePath: "rescued.mkv", ScheduledCheckAt: now, ExpectedStatus: &repairStatus},
		{Type: UpdateTypeRepairRetry, FilePath: "still_repair.mkv", ScheduledCheckAt: now, ExpectedStatus: &repairStatus},
	})
	require.NoError(t, err)

	rescued, err := repo.GetFileHealth(ctx, "rescued.mkv")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusPending, rescued.Status, "guard must prevent clobbering a concurrently-rescued record")
	assert.Equal(t, 1, rescued.RepairRetryCount, "guarded no-op must not increment the repair budget")

	still, err := repo.GetFileHealth(ctx, "still_repair.mkv")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusRepairTriggered, still.Status)
	assert.Equal(t, 2, still.RepairRetryCount, "guard must allow the write when status still matches")
}

// TestUpdateHealthStatusBulk_NoGuardAppliesRegardless verifies that without ExpectedStatus
// the write applies regardless of current status (preserving the legacy unguarded path).
func TestUpdateHealthStatusBulk_NoGuardAppliesRegardless(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, retry_count, max_retries)
		VALUES ('any.mkv', 'pending', 0, 3)
	`)
	require.NoError(t, err)

	err = repo.UpdateHealthStatusBulk(ctx, []HealthStatusUpdate{
		{Type: UpdateTypeRetry, FilePath: "any.mkv", ScheduledCheckAt: time.Now().UTC()},
	})
	require.NoError(t, err)

	h, err := repo.GetFileHealth(ctx, "any.mkv")
	require.NoError(t, err)
	assert.Equal(t, 1, h.RetryCount, "unguarded write must apply")
}

// --- Fix C: relink refuses an ambiguous blanket reset across colliding basenames ---

func TestRelinkFileByFilename_RefusesAmbiguousCollision(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, repair_retry_count, max_repair_retries)
		VALUES ('tv/ShowA/01.mkv', 'repair_triggered', 1, 3), ('tv/ShowB/01.mkv', 'repair_triggered', 1, 3)
	`)
	require.NoError(t, err)

	// Incoming Download whose path matches NEITHER existing record exactly: must refuse.
	relinked, err := repo.RelinkFileByFilename(ctx, "01.mkv", "tv/ShowC/01.mkv", "/lib/ShowC/01.mkv", nil, true)
	require.NoError(t, err)
	assert.False(t, relinked, "a basename matching multiple unrelated records must not be blanket-relinked")

	for _, p := range []string{"tv/ShowA/01.mkv", "tv/ShowB/01.mkv"} {
		h, err := repo.GetFileHealth(ctx, p)
		require.NoError(t, err)
		assert.Equal(t, HealthStatusRepairTriggered, h.Status, "%s must be untouched", p)
		assert.Equal(t, 1, h.RepairRetryCount, "%s repair budget must be untouched", p)
	}
}

func TestRelinkFileByFilename_CollisionResolvedByExactPath(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-5 * time.Minute).Format("2006-01-02 15:04:05")
	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, repair_retry_count, max_repair_retries, updated_at)
		VALUES ('tv/ShowA/01.mkv', 'repair_triggered', 1, 3, ?), ('tv/ShowB/01.mkv', 'repair_triggered', 2, 3, ?)
	`, past, past)
	require.NoError(t, err)

	// Incoming Download whose path matches ShowA exactly: relink only that one.
	relinked, err := repo.RelinkFileByFilename(ctx, "01.mkv", "tv/ShowA/01.mkv", "/lib/ShowA/01.mkv", nil, true)
	require.NoError(t, err)
	assert.True(t, relinked)

	a, err := repo.GetFileHealth(ctx, "tv/ShowA/01.mkv")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusPending, a.Status, "exact-match target is revalidated to pending")
	assert.Equal(t, 1, a.RepairRetryCount, "repair budget preserved across the Download relink")

	b, err := repo.GetFileHealth(ctx, "tv/ShowB/01.mkv")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusRepairTriggered, b.Status, "the colliding sibling must be untouched")
	assert.Equal(t, 2, b.RepairRetryCount)
}

// --- Fix D: prefix deletes escape LIKE metacharacters ---

func TestDeleteHealthRecordsByPrefix_EscapesWildcards(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status) VALUES
			('tv/Show_S01_1080p/e01.mkv', 'pending'),
			('tv/ShowXS01X1080p/e01.mkv', 'pending'),
			('tv/100%Real/e01.mkv', 'pending'),
			('tv/100ZZReal/e01.mkv', 'pending')
	`)
	require.NoError(t, err)

	n, err := repo.DeleteHealthRecordsByPrefix(ctx, "tv/Show_S01_1080p")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "underscore prefix must match only its own subtree")

	survivor, err := repo.GetFileHealth(ctx, "tv/ShowXS01X1080p/e01.mkv")
	require.NoError(t, err)
	assert.NotNil(t, survivor, "'_' wildcard must not over-delete an unrelated sibling")

	n, err = repo.DeleteHealthRecordsByPrefix(ctx, "tv/100%Real")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "percent prefix must match only its own subtree")

	survivor, err = repo.GetFileHealth(ctx, "tv/100ZZReal/e01.mkv")
	require.NoError(t, err)
	assert.NotNil(t, survivor, "'%' wildcard must not over-delete an unrelated sibling")
}

// --- Fix E: failed-import cleanup spares prior successful import's records ---

func TestDeleteUnvalidatedHealthRecordsByPrefix_PreservesValidated(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, library_path, status) VALUES
			('tv/Pack/e01.mkv', 'tv/Pack/e01.mkv', 'pending'),
			('tv/Pack/e02.mkv', '/lib/Pack/e02.mkv', 'healthy'),
			('tv/Pack/e03.mkv', '/lib/Pack/e03.mkv', 'pending'),
			('tv/Pack/e04.mkv', 'tv/Pack/e04.mkv', 'repair_triggered')
	`)
	require.NoError(t, err)

	n, err := repo.DeleteUnvalidatedHealthRecordsByPrefix(ctx, "tv/Pack")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "only the unvalidated placeholder record may be deleted")

	gone, err := repo.GetFileHealth(ctx, "tv/Pack/e01.mkv")
	require.NoError(t, err)
	assert.Nil(t, gone, "pending placeholder (library_path == file_path) is removed")

	for _, p := range []string{"tv/Pack/e02.mkv", "tv/Pack/e03.mkv", "tv/Pack/e04.mkv"} {
		h, err := repo.GetFileHealth(ctx, p)
		require.NoError(t, err)
		assert.NotNil(t, h, "%s belongs to a prior successful import and must survive", p)
	}
}

// --- Fix F: every writer normalizes a leading slash so UNIQUE(file_path) holds ---

func TestRegisterCorruptedFile_NormalizesLeadingSlash(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	require.NoError(t, repo.RegisterCorruptedFile(ctx, "/tv/Slashed/e01.mkv", nil, "boom"))

	h, err := repo.GetFileHealth(ctx, "tv/Slashed/e01.mkv")
	require.NoError(t, err)
	require.NotNil(t, h, "record must be stored without the leading slash")
}

func TestAddHealthCheck_NormalizesLeadingSlash(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	require.NoError(t, repo.AddHealthCheck(ctx, "/tv/Slashed/e02.mkv", now, now, nil))

	h, err := repo.GetFileHealth(ctx, "tv/Slashed/e02.mkv")
	require.NoError(t, err)
	require.NotNil(t, h, "record must be stored without the leading slash")
}

// --- Fix G: bulk checking transition ---

func TestSetFilesCheckingBulk(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status) VALUES ('a.mkv', 'pending'), ('b.mkv', 'pending')
	`)
	require.NoError(t, err)

	require.NoError(t, repo.SetFilesCheckingBulk(ctx, []string{"a.mkv", "/b.mkv"}))

	for _, p := range []string{"a.mkv", "b.mkv"} {
		h, err := repo.GetFileHealth(ctx, p)
		require.NoError(t, err)
		assert.Equal(t, HealthStatusChecking, h.Status, "%s must be marked checking", p)
	}
}

// --- Fix I: batch upsert preserves the per-title repair budget on conflict ---

func TestBatchAddFileToHealthCheck_PreservesRepairBudget(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, retry_count, repair_retry_count, max_repair_retries)
		VALUES ('tv/Pack/old.mkv', 'repair_triggered', 2, 2, 3)
	`)
	require.NoError(t, err)

	lib1 := "tv/Pack/old.mkv"
	lib2 := "tv/Pack/new.mkv"
	err = repo.BatchAddFileToHealthCheck(ctx, []HealthCheckUpsert{
		{FilePath: "tv/Pack/old.mkv", LibraryPath: &lib1, Priority: HealthPriorityNext, MaxRetries: 3, MaxRepairRetries: 3},
		{FilePath: "/tv/Pack/new.mkv", LibraryPath: &lib2, Priority: HealthPriorityNext, MaxRetries: 3, MaxRepairRetries: 3},
	})
	require.NoError(t, err)

	old, err := repo.GetFileHealth(ctx, "tv/Pack/old.mkv")
	require.NoError(t, err)
	require.NotNil(t, old)
	assert.Equal(t, HealthStatusPending, old.Status, "re-import upsert resets to pending")
	assert.Equal(t, 0, old.RetryCount, "retry_count resets for the new copy")
	assert.Equal(t, 2, old.RepairRetryCount, "repair budget must survive the batched re-import upsert")

	fresh, err := repo.GetFileHealth(ctx, "tv/Pack/new.mkv")
	require.NoError(t, err)
	require.NotNil(t, fresh, "new path normalized and inserted")
	assert.Equal(t, HealthStatusPending, fresh.Status)
}
