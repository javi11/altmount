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

func setupPar2RepairSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		CREATE TABLE par2_repair_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT NOT NULL,
			nzb_path TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT,
			failing_segment_id TEXT,
			dead_segment_ids TEXT,
			next_attempt_at TIMESTAMP,
			started_at TIMESTAMP,
			finished_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX idx_par2_repair_active ON par2_repair_jobs(file_path)
			WHERE status IN ('pending','running') AND file_path <> '';
		CREATE UNIQUE INDEX idx_par2_repair_active_nzb ON par2_repair_jobs(nzb_path)
			WHERE status IN ('pending','running') AND nzb_path IS NOT NULL;
		CREATE INDEX idx_par2_repair_due ON par2_repair_jobs(status, next_attempt_at);
	`)
	require.NoError(t, err)
}

func newPar2RepairRepo(t *testing.T) (*Par2RepairRepository, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	setupPar2RepairSchema(t, db)
	return NewPar2RepairRepository(db, DialectSQLite), db
}

func TestPar2RepairEnqueueDedup(t *testing.T) {
	repo, _ := newPar2RepairRepo(t)
	ctx := context.Background()

	created, err := repo.Enqueue(ctx, "/movies/a.mkv", "<seg1@test>")
	require.NoError(t, err)
	assert.True(t, created)

	created, err = repo.Enqueue(ctx, "/movies/a.mkv", "<seg2@test>")
	require.NoError(t, err)
	assert.False(t, created, "second enqueue while pending must dedup")

	// A different file is independent.
	created, err = repo.Enqueue(ctx, "/movies/b.mkv", "")
	require.NoError(t, err)
	assert.True(t, created)
}

func TestPar2RepairClaimNext(t *testing.T) {
	repo, _ := newPar2RepairRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := repo.Enqueue(ctx, "/movies/a.mkv", "<s@test>")
	require.NoError(t, err)
	_, err = repo.Enqueue(ctx, "/movies/b.mkv", "")
	require.NoError(t, err)

	job, err := repo.ClaimNext(ctx, now)
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "/movies/a.mkv", job.FilePath, "oldest job claimed first")
	assert.Equal(t, Par2RepairStatusRunning, job.Status)
	assert.Equal(t, "<s@test>", job.FailingSegmentID.String)

	// While a.mkv is running, next claim gets b.mkv.
	job2, err := repo.ClaimNext(ctx, now)
	require.NoError(t, err)
	require.NotNil(t, job2)
	assert.Equal(t, "/movies/b.mkv", job2.FilePath)

	// Nothing left.
	job3, err := repo.ClaimNext(ctx, now)
	require.NoError(t, err)
	assert.Nil(t, job3)
}

func TestPar2RepairClaimSkipsNotDue(t *testing.T) {
	repo, _ := newPar2RepairRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := repo.Enqueue(ctx, "/movies/a.mkv", "")
	require.NoError(t, err)
	job, err := repo.ClaimNext(ctx, now)
	require.NoError(t, err)
	require.NotNil(t, job)

	// Retry in the future: not claimable now, claimable after due time.
	require.NoError(t, repo.MarkRetry(ctx, job.ID, "transient", now.Add(time.Hour)))

	got, err := repo.ClaimNext(ctx, now)
	require.NoError(t, err)
	assert.Nil(t, got, "job with future next_attempt_at must not be claimed")

	got, err = repo.ClaimNext(ctx, now.Add(2*time.Hour))
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 1, got.Attempts)
	assert.Equal(t, "transient", got.LastError.String)
}

// Installs that ran an older version still carry terminal rows; the startup
// sweep must remove them without touching active jobs.
func TestPar2RepairDeleteFinishedSweepsLegacyTerminalRows(t *testing.T) {
	repo, db := newPar2RepairRepo(t)
	ctx := context.Background()

	_, err := repo.Enqueue(ctx, "/movies/active.mkv", "")
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO par2_repair_jobs (file_path, status) VALUES
		('/movies/old-repaired.mkv', 'repaired'),
		('/movies/old-unrepairable.mkv', 'unrepairable')`)
	require.NoError(t, err)

	require.NoError(t, repo.DeleteFinished(ctx))

	jobs, err := repo.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, "/movies/active.mkv", jobs[0].FilePath)
}

// Finished jobs are deleted (their outcome lives on the health record or the
// import queue entry), so the same file must always be re-queueable.
func TestPar2RepairDeleteAllowsReEnqueue(t *testing.T) {
	repo, _ := newPar2RepairRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := repo.Enqueue(ctx, "/movies/a.mkv", "")
	require.NoError(t, err)
	job, err := repo.ClaimNext(ctx, now)
	require.NoError(t, err)
	require.NoError(t, repo.Delete(ctx, job.ID))

	jobs, err := repo.List(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, jobs, "deleted job must not linger in the list")

	created, err := repo.Enqueue(ctx, "/movies/a.mkv", "")
	require.NoError(t, err)
	assert.True(t, created, "file damaged again must be re-queueable after deletion")
}

// A retry re-runs the work, so the elapsed time shown must be that of the
// attempt in progress, not of the first one.
func TestPar2RepairRetryRestartsRunTime(t *testing.T) {
	repo, _ := newPar2RepairRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := repo.Enqueue(ctx, "/movies/a.mkv", "")
	require.NoError(t, err)
	first, err := repo.ClaimNext(ctx, now)
	require.NoError(t, err)
	require.NoError(t, repo.MarkRetry(ctx, first.ID, "transient", now))

	second, err := repo.ClaimNext(ctx, now.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, second.StartedAt.Valid)
	assert.True(t, second.StartedAt.Time.After(first.StartedAt.Time),
		"a re-claimed job must restart its run clock")
}

func TestPar2RepairAppendDeadSegment(t *testing.T) {
	repo, _ := newPar2RepairRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := repo.Enqueue(ctx, "/movies/a.mkv", "")
	require.NoError(t, err)
	job, err := repo.ClaimNext(ctx, now)
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Empty(t, job.DeadSegments(), "fresh job has no dead segments")

	require.NoError(t, repo.AppendDeadSegment(ctx, job.ID, "<dead1@test>"))
	require.NoError(t, repo.AppendDeadSegment(ctx, job.ID, "<dead2@test>"))
	// Duplicate append must dedup.
	require.NoError(t, repo.AppendDeadSegment(ctx, job.ID, "<dead1@test>"))

	require.NoError(t, repo.MarkRetry(ctx, job.ID, "sweep dead", now))
	got, err := repo.ClaimNext(ctx, now.Add(time.Second))
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, []string{"<dead1@test>", "<dead2@test>"}, got.DeadSegments())
}

func TestPar2RepairDeadSegmentsInvalidJSON(t *testing.T) {
	job := &Par2RepairJob{DeadSegmentIDs: sql.NullString{String: "{not json", Valid: true}}
	assert.Empty(t, job.DeadSegments(), "invalid JSON must yield empty, not panic")
	assert.Empty(t, (&Par2RepairJob{}).DeadSegments(), "NULL must yield empty")
}

func TestPar2RepairList(t *testing.T) {
	repo, _ := newPar2RepairRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := repo.Enqueue(ctx, "/movies/a.mkv", "")
	require.NoError(t, err)
	_, err = repo.Enqueue(ctx, "/movies/b.mkv", "")
	require.NoError(t, err)

	// Touch a.mkv so it becomes the most recently updated (retry not yet due).
	job, err := repo.ClaimNext(ctx, now)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.NoError(t, repo.MarkRetry(ctx, job.ID, "transient", now.Add(time.Hour)))
	// Force a strictly newer updated_at than the untouched row.
	_, err = repo.ClaimNext(ctx, now) // claims b.mkv (running -> excluded from claim but updated)
	require.NoError(t, err)

	jobs, err := repo.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 2)

	// Limit is honored.
	jobs, err = repo.List(ctx, 1)
	require.NoError(t, err)
	require.Len(t, jobs, 1)

	// Zero/negative limit falls back to the default.
	jobs, err = repo.List(ctx, 0)
	require.NoError(t, err)
	require.Len(t, jobs, 2)
}

func TestPar2RepairResetRunning(t *testing.T) {
	repo, _ := newPar2RepairRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := repo.Enqueue(ctx, "/movies/a.mkv", "")
	require.NoError(t, err)
	_, err = repo.ClaimNext(ctx, now)
	require.NoError(t, err)

	require.NoError(t, repo.ResetRunning(ctx))

	job, err := repo.ClaimNext(ctx, now)
	require.NoError(t, err)
	require.NotNil(t, job, "crash-recovered job must be claimable again")
	assert.Equal(t, "/movies/a.mkv", job.FilePath)
}

func TestPar2RepairEnqueueNzb(t *testing.T) {
	repo, _ := newPar2RepairRepo(t)
	ctx := context.Background()

	created, err := repo.EnqueueNzb(ctx, "/nzbs/release.nzb", "<dead@test>")
	require.NoError(t, err)
	assert.True(t, created)

	job, err := repo.ClaimNext(ctx, time.Now().UTC())
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "/nzbs/release.nzb", job.NzbPath.String, "job must carry the NZB source")
	assert.Equal(t, "", job.FilePath, "NZB-mode jobs have no imported file path")

	// Dedup is per NZB path while active.
	created, err = repo.EnqueueNzb(ctx, "/nzbs/release.nzb", "")
	require.NoError(t, err)
	assert.False(t, created)
}
