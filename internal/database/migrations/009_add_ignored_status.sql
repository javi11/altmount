-- +goose Up
-- +goose StatementBegin

-- Create new table with 'ignored' status in CHECK constraint and all current columns
CREATE TABLE file_health_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'checking', 'healthy', 'repair_triggered', 'corrupted', 'ignored')),
    last_checked DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 2,
    repair_retry_count INTEGER NOT NULL DEFAULT 0,
    max_repair_retries INTEGER NOT NULL DEFAULT 3,
    source_nzb_path TEXT DEFAULT NULL,
    error_details TEXT DEFAULT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    release_date DATETIME DEFAULT NULL,
    scheduled_check_at DATETIME DEFAULT NULL,
    library_path TEXT DEFAULT NULL,
    priority INTEGER NOT NULL DEFAULT 0
);

-- Copy data from old table to new table
INSERT INTO file_health_new (
    id, file_path, status, last_checked, last_error, retry_count, max_retries,
    repair_retry_count, max_repair_retries, source_nzb_path,
    error_details, created_at, updated_at, release_date, scheduled_check_at,
    library_path, priority
)
SELECT
    id, file_path, status, last_checked, last_error, retry_count, max_retries,
    repair_retry_count, max_repair_retries, source_nzb_path,
    error_details, created_at, updated_at, release_date, scheduled_check_at,
    library_path, priority
FROM file_health;

-- Drop the old table
DROP TABLE file_health;

-- Rename the new table
ALTER TABLE file_health_new RENAME TO file_health;

-- Recreate indexes
CREATE INDEX idx_file_health_status ON file_health(status);
CREATE INDEX idx_file_health_path ON file_health(file_path);
CREATE INDEX idx_file_health_source ON file_health(source_nzb_path);
CREATE INDEX idx_file_health_updated ON file_health(updated_at);
CREATE INDEX idx_file_health_scheduled ON file_health(scheduled_check_at) WHERE scheduled_check_at IS NOT NULL;

-- Recreate the update trigger
CREATE TRIGGER update_file_health_timestamp
AFTER UPDATE ON file_health
BEGIN
    UPDATE file_health SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Revert to previous table structure without 'ignored' status
CREATE TABLE file_health_original (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'checking', 'healthy', 'repair_triggered', 'corrupted')),
    last_checked DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 2,
    repair_retry_count INTEGER NOT NULL DEFAULT 0,
    max_repair_retries INTEGER NOT NULL DEFAULT 3,
    source_nzb_path TEXT DEFAULT NULL,
    error_details TEXT DEFAULT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    release_date DATETIME DEFAULT NULL,
    scheduled_check_at DATETIME DEFAULT NULL,
    library_path TEXT DEFAULT NULL,
    priority INTEGER NOT NULL DEFAULT 0
);

-- Copy data back, mapping 'ignored' to 'pending'
INSERT INTO file_health_original (
    id, file_path, status, last_checked, last_error, retry_count, max_retries,
    repair_retry_count, max_repair_retries, source_nzb_path,
    error_details, created_at, updated_at, release_date, scheduled_check_at,
    library_path, priority
)
SELECT
    id, file_path,
    CASE
        WHEN status = 'ignored' THEN 'pending'
        ELSE status
    END,
    last_checked, last_error, retry_count, max_retries,
    repair_retry_count, max_repair_retries, source_nzb_path,
    error_details, created_at, updated_at, release_date, scheduled_check_at,
    library_path, priority
FROM file_health;

-- Drop current table
DROP TABLE file_health;
ALTER TABLE file_health_original RENAME TO file_health;

-- Recreate indexes
CREATE INDEX idx_file_health_status ON file_health(status);
CREATE INDEX idx_file_health_path ON file_health(file_path);
CREATE INDEX idx_file_health_source ON file_health(source_nzb_path);
CREATE INDEX idx_file_health_updated ON file_health(updated_at);
CREATE INDEX idx_file_health_scheduled ON file_health(scheduled_check_at) WHERE scheduled_check_at IS NOT NULL;

-- Recreate trigger
CREATE TRIGGER update_file_health_timestamp
AFTER UPDATE ON file_health
BEGIN
    UPDATE file_health SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- +goose StatementEnd