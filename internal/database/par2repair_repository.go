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

const (
	Par2RepairStatusPending      Par2RepairStatus = "pending"
	Par2RepairStatusRunning      Par2RepairStatus = "running"
	Par2RepairStatusRepaired     Par2RepairStatus = "repaired"
	Par2RepairStatusUnrepairable Par2RepairStatus = "unrepairable"
)

// Par2RepairJob is one row of par2_repair_jobs.
type Par2RepairJob struct {
	ID               int64
	FilePath         string
	Status           Par2RepairStatus
	Attempts         int
	LastError        sql.NullString
	FailingSegmentID sql.NullString
	DeadSegmentIDs   sql.NullString // JSON array of message IDs found dead mid-repair
	NextAttemptAt    sql.NullTime
	CreatedAt        time.Time
	UpdatedAt        time.Time
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

// Enqueue inserts a pending job for the file unless one is already active.
// Returns created=false when an active job made this a no-op.
func (r *Par2RepairRepository) Enqueue(ctx context.Context, filePath string, failingSegmentID string) (bool, error) {
	if filePath == "" {
		return false, errors.New("par2repair: empty file path")
	}
	var segID any
	if failingSegmentID != "" {
		segID = failingSegmentID
	}
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO par2_repair_jobs (file_path, status, failing_segment_id)
		SELECT ?, 'pending', ?
		WHERE NOT EXISTS (
			SELECT 1 FROM par2_repair_jobs
			WHERE file_path = ? AND status IN ('pending','running')
		)`, filePath, segID, filePath)
	if err != nil {
		return false, fmt.Errorf("enqueue par2 repair: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("enqueue par2 repair: %w", err)
	}
	return n > 0, nil
}

// ClaimNext atomically claims the oldest due pending job (pending and either
// no next_attempt_at or next_attempt_at <= now), flips it to running and
// returns it. Returns nil when no job is due.
func (r *Par2RepairRepository) ClaimNext(ctx context.Context, now time.Time) (*Par2RepairJob, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE par2_repair_jobs
		SET status = 'running', updated_at = ?
		WHERE id = (
			SELECT id FROM par2_repair_jobs
			WHERE status = 'pending'
			  AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
			ORDER BY id ASC
			LIMIT 1
		)
		RETURNING id, file_path, status, attempts, last_error, failing_segment_id,
		          dead_segment_ids, next_attempt_at, created_at, updated_at`, now, now)

	job := &Par2RepairJob{}
	err := row.Scan(&job.ID, &job.FilePath, &job.Status, &job.Attempts, &job.LastError,
		&job.FailingSegmentID, &job.DeadSegmentIDs, &job.NextAttemptAt, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim par2 repair job: %w", err)
	}
	return job, nil
}

// MarkRepaired finishes a job successfully.
func (r *Par2RepairRepository) MarkRepaired(ctx context.Context, id int64) error {
	return r.setTerminal(ctx, id, Par2RepairStatusRepaired, "")
}

// MarkUnrepairable finishes a job as permanently failed with a reason.
func (r *Par2RepairRepository) MarkUnrepairable(ctx context.Context, id int64, reason string) error {
	return r.setTerminal(ctx, id, Par2RepairStatusUnrepairable, reason)
}

func (r *Par2RepairRepository) setTerminal(ctx context.Context, id int64, status Par2RepairStatus, reason string) error {
	var lastErr any
	if reason != "" {
		lastErr = reason
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE par2_repair_jobs
		SET status = ?, last_error = COALESCE(?, last_error), updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, string(status), lastErr, id)
	if err != nil {
		return fmt.Errorf("finish par2 repair job %d: %w", id, err)
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
		SELECT id, file_path, status, attempts, last_error, failing_segment_id,
		       dead_segment_ids, next_attempt_at, created_at, updated_at
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
		if err := rows.Scan(&job.ID, &job.FilePath, &job.Status, &job.Attempts, &job.LastError,
			&job.FailingSegmentID, &job.DeadSegmentIDs, &job.NextAttemptAt, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan par2 repair job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list par2 repair jobs: %w", err)
	}
	return jobs, nil
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
