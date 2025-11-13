-- +goose Up
-- +goose StatementBegin

-- Convert existing 'partial' status records to 'corrupted'
-- Any file with missing segments is considered corrupted
UPDATE file_health
SET status = 'corrupted',
    updated_at = datetime('now')
WHERE status = 'partial';

-- Update the CHECK constraint to remove 'partial' status
-- Create a new table with the updated constraint
CREATE TABLE file_health_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'checking', 'healthy', 'repair_triggered', 'corrupted')),
    last_checked DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 2,
    repair_retry_count INTEGER NOT NULL DEFAULT 0,
    max_repair_retries INTEGER NOT NULL DEFAULT 3,
    next_retry_at DATETIME DEFAULT NULL,
    source_nzb_path TEXT DEFAULT NULL,
    error_details TEXT DEFAULT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Copy data from old table to new table
INSERT INTO file_health_new (
    id, file_path, status, last_checked, last_error, retry_count, max_retries,
    repair_retry_count, max_repair_retries, next_retry_at, source_nzb_path,
    error_details, created_at, updated_at
)
SELECT
    id, file_path, status, last_checked, last_error, retry_count, max_retries,
    repair_retry_count, max_repair_retries, next_retry_at, source_nzb_path,
    error_details, created_at, updated_at
FROM file_health;

-- Drop the old table
DROP TABLE file_health;

-- Rename the new table
ALTER TABLE file_health_new RENAME TO file_health;

-- Recreate indexes for the new table
CREATE INDEX idx_file_health_status ON file_health(status);
CREATE INDEX idx_file_health_retry ON file_health(status, next_retry_at) WHERE status NOT IN ('healthy', 'checking');
CREATE INDEX idx_file_health_path ON file_health(file_path);
CREATE INDEX idx_file_health_source ON file_health(source_nzb_path);
CREATE INDEX idx_file_health_updated ON file_health(updated_at);

-- Recreate the update trigger
CREATE TRIGGER update_file_health_timestamp
AFTER UPDATE ON file_health
BEGIN
    UPDATE file_health SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Revert to previous file_health table structure with 'partial' status
CREATE TABLE file_health_original (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'checking', 'healthy', 'partial', 'repair_triggered', 'corrupted')),
    last_checked DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 2,
    repair_retry_count INTEGER NOT NULL DEFAULT 0,
    max_repair_retries INTEGER NOT NULL DEFAULT 3,
    next_retry_at DATETIME DEFAULT NULL,
    source_nzb_path TEXT DEFAULT NULL,
    error_details TEXT DEFAULT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Copy data back
INSERT INTO file_health_original (
    id, file_path, status, last_checked, last_error, retry_count, max_retries,
    repair_retry_count, max_repair_retries, next_retry_at, source_nzb_path,
    error_details, created_at, updated_at
)
SELECT
    id, file_path, status, last_checked, last_error, retry_count, max_retries,
    repair_retry_count, max_repair_retries, next_retry_at, source_nzb_path,
    error_details, created_at, updated_at
FROM file_health;

-- Drop current table and restore original
DROP TABLE file_health;
ALTER TABLE file_health_original RENAME TO file_health;

-- Recreate original indexes
CREATE INDEX idx_file_health_status ON file_health(status);
CREATE INDEX idx_file_health_retry ON file_health(status, next_retry_at) WHERE status NOT IN ('healthy', 'checking');
CREATE INDEX idx_file_health_path ON file_health(file_path);
CREATE INDEX idx_file_health_source ON file_health(source_nzb_path);
CREATE INDEX idx_file_health_updated ON file_health(updated_at);

-- Recreate the update trigger
CREATE TRIGGER update_file_health_timestamp
AFTER UPDATE ON file_health
BEGIN
    UPDATE file_health SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- +goose StatementEnd
