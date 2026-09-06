package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Par2RepairStatus is the lifecycle state of a PAR2 repair job.
type Par2RepairStatus string

// Only active states exist: a finished job's outcome is translated to the
// file's health record (file mode) or the import queue entry (NZB mode) and
// the row is deleted.
const (
	Par2RepairStatusPending Par2RepairStatus = "pending"
	Par2RepairStatusRunning Par2RepairStatus = "running"
)

// Par2RepairJob is one row of par2_repair_jobs.
type Par2RepairJob struct {
	ID       int64
	FilePath string
	NzbPath  sql.NullString // set for NZB-mode jobs (release never imported)
	// ReleaseRef is the release's shared NzbStore ref; files with the same ref
	// group onto one job because a repair sweeps the whole release anyway.
	ReleaseRef sql.NullString
	// MemberPaths is the JSON list of every file the job repairs on behalf of
	// (the trigger file included). NULL on legacy and NZB-mode rows.
	MemberPaths      sql.NullString
	Status           Par2RepairStatus
	Attempts         int
	LastError        sql.NullString
	FailingSegmentID sql.NullString
	DeadSegmentIDs   sql.NullString // JSON array of message IDs found dead mid-repair
	NextAttemptAt    sql.NullTime
	StartedAt        sql.NullTime // when the current/last attempt began running
	FinishedAt       sql.NullTime // when the job reached a terminal state
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// RunDuration reports how long the job's last attempt ran: the finished
// duration for a terminal job, or the elapsed time so far for a running one.
// Returns false when the job has not started.
func (j *Par2RepairJob) RunDuration(now time.Time) (time.Duration, bool) {
	if !j.StartedAt.Valid {
		return 0, false
	}
	end := now
	if j.FinishedAt.Valid {
		end = j.FinishedAt.Time
	}
	d := end.Sub(j.StartedAt.Time)
	if d < 0 {
		return 0, false
	}
	return d, true
}

// DeadSegments unmarshals the persisted dead-segment message IDs. Returns an
// empty slice on NULL or invalid JSON.
func (j *Par2RepairJob) DeadSegments() []string {
	if !j.DeadSegmentIDs.Valid || j.DeadSegmentIDs.String == "" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(j.DeadSegmentIDs.String), &ids); err != nil {
		return nil
	}
	return ids
}

// Members returns every file the job repairs on behalf of: the grouped member
// list when present, else the trigger file. Empty for NZB-mode jobs.
func (j *Par2RepairJob) Members() []string {
	if j.MemberPaths.Valid && j.MemberPaths.String != "" {
		var paths []string
		if err := json.Unmarshal([]byte(j.MemberPaths.String), &paths); err == nil && len(paths) > 0 {
			return paths
		}
	}
	if j.FilePath == "" {
		return nil
	}
	return []string{j.FilePath}
}

// Par2RepairRepository persists PAR2 repair jobs so pending repairs survive
// restarts. At most one active (pending/running) job exists per file, enforced
// by a partial unique index.
type Par2RepairRepository struct {
	db DBQuerier
}

// NewPar2RepairRepository creates a repository over the given database.
func NewPar2RepairRepository(db *sql.DB, d Dialect) *Par2RepairRepository {
	return &Par2RepairRepository{db: newDialectAwareDB(db, d)}
}

// Enqueue registers a file for repair. releaseRef (the release's NzbStore ref)
// groups files: when an active job for the same release exists, the file is
// attached to it instead of creating a second row — one repair sweeps the
// whole release, so N rows would just show the user N copies of the same work.
// An empty releaseRef falls back to per-file dedup.
//
// Returns created=true when a new job row was inserted, attached=true when the
// file joined an existing job, and both false when it was already tracked.
func (r *Par2RepairRepository) Enqueue(ctx context.Context, filePath, releaseRef, failingSegmentID string) (created, attached bool, err error) {
	if filePath == "" {
		return false, false, errors.New("par2repair: empty file path")
	}
	var segID any
	if failingSegmentID != "" {
		segID = failingSegmentID
	}
	var relRef any
	if releaseRef != "" {
		relRef = releaseRef
	}
	memberJSON, err := json.Marshal([]string{filePath})
	if err != nil {
		return false, false, fmt.Errorf("enqueue par2 repair: %w", err)
	}
	// Attach and insert race with concurrent enqueues and with the job
	// finishing; the optimistic attach guard and the partial unique indexes
	// make the loser take another lap rather than duplicate or clobber.
	for range 3 {
		if releaseRef != "" {
			done, att, err := r.tryAttach(ctx, filePath, releaseRef, failingSegmentID)
			if err != nil {
				return false, false, err
			}
			if done {
				return false, att, nil
			}
		}
		res, err := r.db.ExecContext(ctx, `
			INSERT INTO par2_repair_jobs (file_path, release_ref, member_paths, status, failing_segment_id)
			SELECT ?, ?, ?, 'pending', ?
			WHERE NOT EXISTS (
				SELECT 1 FROM par2_repair_jobs
				WHERE file_path = ? AND status IN ('pending','running')
			) AND NOT EXISTS (
				SELECT 1 FROM par2_repair_jobs
				WHERE release_ref = ? AND status IN ('pending','running')
			)`, filePath, relRef, string(memberJSON), segID, filePath, releaseRef)
		if err != nil {
			return false, false, fmt.Errorf("enqueue par2 repair: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return false, false, fmt.Errorf("enqueue par2 repair: %w", err)
		}
		if n > 0 {
			return true, false, nil
		}
		if releaseRef == "" {
			return false, false, nil // per-file dedup: already queued
		}
		// An active job for the file or release appeared mid-flight: attach to
		// it on the next lap.
	}
	return false, false, errors.New("par2repair: enqueue contention, giving up")
}

// tryAttach joins filePath to the release's active job, folding its failing
// segment into the job's known-dead list so the plan covers it up front.
// done=true means the enqueue is fully handled (attached, or already a
// member); done=false means no active job exists or it changed under us.
func (r *Par2RepairRepository) tryAttach(ctx context.Context, filePath, releaseRef, failingSegmentID string) (done, attached bool, err error) {
	var id int64
	var trigger string
	var members, deads sql.NullString
	err = r.db.QueryRowContext(ctx, `
		SELECT id, file_path, member_paths, dead_segment_ids FROM par2_repair_jobs
		WHERE release_ref = ? AND status IN ('pending','running')`, releaseRef).
		Scan(&id, &trigger, &members, &deads)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("find release repair job: %w", err)
	}

	job := &Par2RepairJob{FilePath: trigger, MemberPaths: members, DeadSegmentIDs: deads}
	paths := job.Members()
	if slices.Contains(paths, filePath) {
		return true, false, nil
	}
	paths = append(paths, filePath)
	pathsJSON, err := json.Marshal(paths)
	if err != nil {
		return false, false, fmt.Errorf("attach to par2 repair job %d: %w", id, err)
	}
	var deadVal any
	if deads.Valid {
		deadVal = deads.String
	}
	if failingSegmentID != "" {
		ids := job.DeadSegments()
		if !slices.Contains(ids, failingSegmentID) {
			buf, err := json.Marshal(append(ids, failingSegmentID))
			if err != nil {
				return false, false, fmt.Errorf("attach to par2 repair job %d: %w", id, err)
			}
			deadVal = string(buf)
		}
	}
	// Optimistic guard on both JSON columns: a concurrent attach or dead-
	// segment append between the read and this write loses nothing — the
	// update misses and the caller takes another lap.
	res, err := r.db.ExecContext(ctx, `
		UPDATE par2_repair_jobs
		SET member_paths = ?, dead_segment_ids = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN ('pending','running')
		  AND COALESCE(member_paths, '') = COALESCE(?, '')
		  AND COALESCE(dead_segment_ids, '') = COALESCE(?, '')`,
		string(pathsJSON), deadVal, id, nullableString(members), nullableString(deads))
	if err != nil {
		return false, false, fmt.Errorf("attach to par2 repair job %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, false, fmt.Errorf("attach to par2 repair job %d: %w", id, err)
	}
	return n > 0, n > 0, nil
}

// nullableString converts a NullString to a driver-friendly any (nil on NULL).
func nullableString(s sql.NullString) any {
	if !s.Valid {
		return nil
	}
	return s.String
}

// EnqueueNzb inserts a pending NZB-mode job — a repair for a release that was
// never imported, planned straight from the NZB. Dedup is per NZB path.
func (r *Par2RepairRepository) EnqueueNzb(ctx context.Context, nzbPath string, failingSegmentID string) (bool, error) {
	if nzbPath == "" {
		return false, errors.New("par2repair: empty nzb path")
	}
	var segID any
	if failingSegmentID != "" {
		segID = failingSegmentID
	}
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO par2_repair_jobs (file_path, nzb_path, status, failing_segment_id)
		SELECT '', ?, 'pending', ?
		WHERE NOT EXISTS (
			SELECT 1 FROM par2_repair_jobs
			WHERE nzb_path = ? AND status IN ('pending','running')
		)`, nzbPath, segID, nzbPath)
	if err != nil {
		return false, fmt.Errorf("enqueue nzb par2 repair: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("enqueue nzb par2 repair: %w", err)
	}
	return n > 0, nil
}

// ClaimNext atomically claims the oldest due pending job (pending and either
// no next_attempt_at or next_attempt_at <= now), flips it to running and
// returns it. Returns nil when no job is due.
func (r *Par2RepairRepository) ClaimNext(ctx context.Context, now time.Time) (*Par2RepairJob, error) {
	// started_at is stamped on every claim and finished_at cleared: a retry
	// re-runs the whole sweep, so the run clock belongs to this attempt.
	row := r.db.QueryRowContext(ctx, `
		UPDATE par2_repair_jobs
		SET status = 'running', updated_at = ?, started_at = ?, finished_at = NULL
		WHERE id = (
			SELECT id FROM par2_repair_jobs
			WHERE status = 'pending'
			  AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
			ORDER BY id ASC
			LIMIT 1
		)
		RETURNING id, file_path, nzb_path, release_ref, member_paths, status, attempts, last_error, failing_segment_id,
		          dead_segment_ids, next_attempt_at, started_at, finished_at, created_at, updated_at`,
		now, now, now)

	job := &Par2RepairJob{}
	err := row.Scan(&job.ID, &job.FilePath, &job.NzbPath, &job.ReleaseRef, &job.MemberPaths, &job.Status, &job.Attempts, &job.LastError,
		&job.FailingSegmentID, &job.DeadSegmentIDs, &job.NextAttemptAt,
		&job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim par2 repair job: %w", err)
	}
	return job, nil
}

// DeleteFinished removes every job in a terminal state. Terminal rows are no
// longer written, but installs that ran an older version still carry them;
// the service sweeps once at startup.
func (r *Par2RepairRepository) DeleteFinished(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM par2_repair_jobs WHERE status NOT IN ('pending','running')`)
	if err != nil {
		return fmt.Errorf("delete finished par2 repair jobs: %w", err)
	}
	return nil
}

// Delete removes a finished job. Rows are working state, not history: a
// terminal outcome is translated to the file's health record (file mode) or
// the import queue entry (NZB mode) before the row is deleted.
func (r *Par2RepairRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM par2_repair_jobs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete par2 repair job %d: %w", id, err)
	}
	return nil
}

// MarkRetry returns a running job to pending with a backoff due time.
func (r *Par2RepairRepository) MarkRetry(ctx context.Context, id int64, reason string, nextAttempt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE par2_repair_jobs
		SET status = 'pending', attempts = attempts + 1, last_error = ?,
		    next_attempt_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, reason, nextAttempt, id)
	if err != nil {
		return fmt.Errorf("retry par2 repair job %d: %w", id, err)
	}
	return nil
}

// AppendDeadSegment persists a message ID discovered dead mid-repair on the
// job row (deduplicated), so the next attempt plans it as missing up front.
func (r *Par2RepairRepository) AppendDeadSegment(ctx context.Context, id int64, messageID string) error {
	if messageID == "" {
		return errors.New("par2repair: empty message ID")
	}
	var raw sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT dead_segment_ids FROM par2_repair_jobs WHERE id = ?`, id).Scan(&raw)
	if err != nil {
		return fmt.Errorf("read dead segments for par2 repair job %d: %w", id, err)
	}
	ids := (&Par2RepairJob{DeadSegmentIDs: raw}).DeadSegments()
	if slices.Contains(ids, messageID) {
		return nil
	}
	ids = append(ids, messageID)
	buf, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("marshal dead segments for par2 repair job %d: %w", id, err)
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE par2_repair_jobs
		SET dead_segment_ids = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, string(buf), id)
	if err != nil {
		return fmt.Errorf("append dead segment to par2 repair job %d: %w", id, err)
	}
	return nil
}

const par2RepairListDefaultLimit = 100

// List returns the most recently updated repair jobs, newest first. limit <= 0
// or > 100 falls back to the default cap of 100.
func (r *Par2RepairRepository) List(ctx context.Context, limit int) ([]*Par2RepairJob, error) {
	if limit <= 0 || limit > par2RepairListDefaultLimit {
		limit = par2RepairListDefaultLimit
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, file_path, nzb_path, release_ref, member_paths, status, attempts, last_error, failing_segment_id,
		       dead_segment_ids, next_attempt_at, started_at, finished_at, created_at, updated_at
		FROM par2_repair_jobs
		ORDER BY updated_at DESC, id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list par2 repair jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*Par2RepairJob
	for rows.Next() {
		job := &Par2RepairJob{}
		if err := rows.Scan(&job.ID, &job.FilePath, &job.NzbPath, &job.ReleaseRef, &job.MemberPaths, &job.Status, &job.Attempts, &job.LastError,
			&job.FailingSegmentID, &job.DeadSegmentIDs, &job.NextAttemptAt,
			&job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan par2 repair job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list par2 repair jobs: %w", err)
	}
	return jobs, nil
}

// Get returns one job by ID, or (nil, nil) when no such row exists.
func (r *Par2RepairRepository) Get(ctx context.Context, id int64) (*Par2RepairJob, error) {
	job := &Par2RepairJob{}
	err := r.db.QueryRowContext(ctx, `
		SELECT id, file_path, nzb_path, release_ref, member_paths, status, attempts, last_error, failing_segment_id,
		       dead_segment_ids, next_attempt_at, started_at, finished_at, created_at, updated_at
		FROM par2_repair_jobs
		WHERE id = ?`, id).Scan(
		&job.ID, &job.FilePath, &job.NzbPath, &job.ReleaseRef, &job.MemberPaths, &job.Status, &job.Attempts, &job.LastError,
		&job.FailingSegmentID, &job.DeadSegmentIDs, &job.NextAttemptAt,
		&job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get par2 repair job: %w", err)
	}
	return job, nil
}

// ResetRunning flips running jobs back to pending; called once at startup so
// jobs interrupted by a crash or shutdown are re-claimed.
func (r *Par2RepairRepository) ResetRunning(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE par2_repair_jobs
		SET status = 'pending', updated_at = CURRENT_TIMESTAMP
		WHERE status = 'running'`)
	if err != nil {
		return fmt.Errorf("reset running par2 repair jobs: %w", err)
	}
	return nil
}
