-- +goose Up
CREATE TABLE par2_repair_jobs (
    id BIGSERIAL PRIMARY KEY,
    file_path TEXT NOT NULL,
    nzb_path TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    failing_segment_id TEXT,
    dead_segment_ids TEXT,
    next_attempt_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- One active (pending/running) job per file; terminal jobs keep history.
CREATE UNIQUE INDEX idx_par2_repair_active ON par2_repair_jobs(file_path)
    WHERE status IN ('pending','running') AND file_path <> '';
CREATE UNIQUE INDEX idx_par2_repair_active_nzb ON par2_repair_jobs(nzb_path)
    WHERE status IN ('pending','running') AND nzb_path IS NOT NULL;
CREATE INDEX idx_par2_repair_due ON par2_repair_jobs(status, next_attempt_at);

-- +goose Down
DROP INDEX IF EXISTS idx_par2_repair_due;
DROP INDEX IF EXISTS idx_par2_repair_active;
DROP TABLE IF EXISTS par2_repair_jobs;
