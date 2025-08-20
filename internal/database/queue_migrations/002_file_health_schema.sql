-- +goose Up
-- +goose StatementBegin
CREATE TABLE file_health (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL UNIQUE, -- Virtual file path in the filesystem
    status TEXT NOT NULL DEFAULT 'healthy' CHECK(status IN ('healthy', 'partial', 'corrupted')),
    last_checked DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 5,
    next_retry_at DATETIME DEFAULT NULL,
    source_nzb_path TEXT DEFAULT NULL, -- Source NZB file for reference
    error_details TEXT DEFAULT NULL, -- JSON error details (missing segments, etc)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for efficient health monitoring
CREATE INDEX idx_file_health_status ON file_health(status);
CREATE INDEX idx_file_health_retry ON file_health(status, next_retry_at) WHERE status != 'healthy';
CREATE INDEX idx_file_health_path ON file_health(file_path);
CREATE INDEX idx_file_health_source ON file_health(source_nzb_path);
CREATE INDEX idx_file_health_updated ON file_health(updated_at);

-- Trigger to update updated_at on record changes
CREATE TRIGGER update_file_health_timestamp 
AFTER UPDATE ON file_health
BEGIN
    UPDATE file_health SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS update_file_health_timestamp;
DROP INDEX IF EXISTS idx_file_health_updated;
DROP INDEX IF EXISTS idx_file_health_source;
DROP INDEX IF EXISTS idx_file_health_path;
DROP INDEX IF EXISTS idx_file_health_retry;
DROP INDEX IF EXISTS idx_file_health_status;
DROP TABLE IF EXISTS file_health;
-- +goose StatementEnd