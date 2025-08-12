-- +goose Up
-- +goose StatementBegin
CREATE TABLE import_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    nzb_path TEXT NOT NULL,
    watch_root TEXT DEFAULT NULL,
    priority INTEGER NOT NULL DEFAULT 1, -- 1=high, 2=normal, 3=low
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'completed', 'failed', 'retrying')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME DEFAULT NULL,
    completed_at DATETIME DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3,
    error_message TEXT DEFAULT NULL,
    batch_id TEXT DEFAULT NULL, -- Optional batch identifier for group processing
    metadata TEXT DEFAULT NULL, -- JSON metadata for additional processing options
    UNIQUE(nzb_path) -- Prevent duplicate entries for same file
);

-- Indexes for efficient queue processing
CREATE INDEX idx_queue_status_priority ON import_queue(status, priority, created_at);
CREATE INDEX idx_queue_batch_id ON import_queue(batch_id);
CREATE INDEX idx_queue_status ON import_queue(status);
CREATE INDEX idx_queue_retry ON import_queue(status, retry_count, max_retries);
CREATE INDEX idx_queue_nzb_path ON import_queue(nzb_path);

-- Queue statistics table for monitoring
CREATE TABLE queue_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    total_queued INTEGER NOT NULL DEFAULT 0,
    total_processing INTEGER NOT NULL DEFAULT 0,
    total_completed INTEGER NOT NULL DEFAULT 0,
    total_failed INTEGER NOT NULL DEFAULT 0,
    avg_processing_time_ms INTEGER DEFAULT NULL,
    last_updated DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Initialize stats table with default row
INSERT INTO queue_stats (total_queued, total_processing, total_completed, total_failed) 
VALUES (0, 0, 0, 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_queue_nzb_path;
DROP INDEX IF EXISTS idx_queue_retry;
DROP INDEX IF EXISTS idx_queue_status;
DROP INDEX IF EXISTS idx_queue_batch_id;
DROP INDEX IF EXISTS idx_queue_status_priority;
DROP TABLE IF EXISTS queue_stats;
DROP TABLE IF EXISTS import_queue;
-- +goose StatementEnd