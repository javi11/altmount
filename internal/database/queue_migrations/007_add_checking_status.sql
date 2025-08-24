-- +goose Up
-- +goose StatementBegin

-- Update status constraint to include 'checking'

-- Create temporary table with new constraint that includes 'checking'
CREATE TABLE file_health_temp (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'checking', 'healthy', 'partial', 'corrupted')),
    last_checked DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 5,
    next_retry_at DATETIME DEFAULT NULL,
    source_nzb_path TEXT DEFAULT NULL,
    error_details TEXT DEFAULT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Copy existing data to temporary table
INSERT INTO file_health_temp 
SELECT id, file_path, status, last_checked, last_error, retry_count, max_retries, 
       next_retry_at, source_nzb_path, error_details, created_at, updated_at
FROM file_health;

-- Drop original table
DROP TABLE file_health;

-- Rename temporary table to original name
ALTER TABLE file_health_temp RENAME TO file_health;

-- Recreate indexes
CREATE INDEX idx_file_health_status ON file_health(status);
CREATE INDEX idx_file_health_retry ON file_health(status, next_retry_at) WHERE status NOT IN ('healthy', 'checking');
CREATE INDEX idx_file_health_path ON file_health(file_path);
CREATE INDEX idx_file_health_source ON file_health(source_nzb_path);
CREATE INDEX idx_file_health_updated ON file_health(updated_at);

-- Recreate trigger
CREATE TRIGGER update_file_health_timestamp 
AFTER UPDATE ON file_health
BEGIN
    UPDATE file_health SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Create temporary table with previous constraint (without 'checking')
CREATE TABLE file_health_temp (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'healthy', 'partial', 'corrupted')),
    last_checked DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 5,
    next_retry_at DATETIME DEFAULT NULL,
    source_nzb_path TEXT DEFAULT NULL,
    error_details TEXT DEFAULT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Convert any 'checking' status to 'pending' when rolling back
INSERT INTO file_health_temp 
SELECT id, file_path, 
       CASE WHEN status = 'checking' THEN 'pending' ELSE status END,
       last_checked, last_error, retry_count, max_retries, 
       next_retry_at, source_nzb_path, error_details, created_at, updated_at
FROM file_health;

-- Drop current table
DROP TABLE file_health;

-- Rename temporary table to original name
ALTER TABLE file_health_temp RENAME TO file_health;

-- Recreate indexes
CREATE INDEX idx_file_health_status ON file_health(status);
CREATE INDEX idx_file_health_retry ON file_health(status, next_retry_at) WHERE status != 'healthy';
CREATE INDEX idx_file_health_path ON file_health(file_path);
CREATE INDEX idx_file_health_source ON file_health(source_nzb_path);
CREATE INDEX idx_file_health_updated ON file_health(updated_at);

-- Recreate trigger
CREATE TRIGGER update_file_health_timestamp 
AFTER UPDATE ON file_health
BEGIN
    UPDATE file_health SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- +goose StatementEnd