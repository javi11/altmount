-- +goose Up
CREATE TABLE par2_repair_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    failing_segment_id TEXT,
    dead_segment_ids TEXT,
    next_attempt_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- One active (pending/running) job per file; terminal jobs keep history.
CREATE UNIQUE INDEX idx_par2_repair_active ON par2_repair_jobs(file_path)
    WHERE status IN ('pending','running');
CREATE INDEX idx_par2_repair_due ON par2_repair_jobs(status, next_attempt_at);

-- +goose Down
DROP INDEX IF EXISTS idx_par2_repair_due;
DROP INDEX IF EXISTS idx_par2_repair_active;
DROP TABLE IF EXISTS par2_repair_jobs;
