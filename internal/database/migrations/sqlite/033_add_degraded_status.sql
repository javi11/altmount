-- +goose Up
-- +goose StatementBegin

-- Add the 'degraded' status: file has missing media-payload segments but is
-- still playable (short glitch / broken seeking). Degraded files stay visible
-- and streamable and do NOT trigger ARR repair.
--
-- SQLite CHECK constraints are immutable, so the table is rebuilt with the
-- widened constraint. The column list, indexes and trigger below replicate
-- the exact live schema produced by migrations 001-032.
CREATE TABLE file_health_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'checking', 'healthy', 'repair_triggered', 'corrupted', 'degraded')),
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
    release_date DATETIME,
    scheduled_check_at DATETIME,
    library_path TEXT DEFAULT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    streaming_failure_count INTEGER DEFAULT 0,
    is_masked BOOLEAN DEFAULT FALSE,
    metadata JSONB DEFAULT NULL,
    indexer TEXT DEFAULT NULL
);

INSERT INTO file_health_new (
    id, file_path, status, last_checked, last_error, retry_count, max_retries,
    repair_retry_count, max_repair_retries, source_nzb_path, error_details,
    created_at, updated_at, release_date, scheduled_check_at, library_path,
    priority, streaming_failure_count, is_masked, metadata, indexer
)
SELECT
    id, file_path, status, last_checked, last_error, retry_count, max_retries,
    repair_retry_count, max_repair_retries, source_nzb_path, error_details,
    created_at, updated_at, release_date, scheduled_check_at, library_path,
    priority, streaming_failure_count, is_masked, metadata, indexer
FROM file_health;

DROP TABLE file_health;
ALTER TABLE file_health_new RENAME TO file_health;

CREATE INDEX idx_file_health_status ON file_health(status);
CREATE INDEX idx_file_health_path ON file_health(file_path);
CREATE INDEX idx_file_health_source ON file_health(source_nzb_path);
CREATE INDEX idx_file_health_updated ON file_health(updated_at);
CREATE INDEX idx_file_health_library_path ON file_health(library_path);
CREATE INDEX idx_file_health_masked ON file_health(is_masked) WHERE is_masked = TRUE;
CREATE INDEX idx_file_health_indexer ON file_health(indexer);
CREATE INDEX idx_file_health_release_date
    ON file_health(release_date)
    WHERE release_date IS NOT NULL;
CREATE INDEX idx_file_health_scheduled
    ON file_health(scheduled_check_at)
    WHERE scheduled_check_at IS NOT NULL;
CREATE INDEX idx_file_health_due
    ON file_health(priority DESC, scheduled_check_at ASC)
    WHERE scheduled_check_at IS NOT NULL;

CREATE TRIGGER update_file_health_timestamp
AFTER UPDATE ON file_health
BEGIN
    UPDATE file_health SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

UPDATE file_health SET status = 'corrupted', updated_at = CURRENT_TIMESTAMP WHERE status = 'degraded';

CREATE TABLE file_health_old (
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
    release_date DATETIME,
    scheduled_check_at DATETIME,
    library_path TEXT DEFAULT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    streaming_failure_count INTEGER DEFAULT 0,
    is_masked BOOLEAN DEFAULT FALSE,
    metadata JSONB DEFAULT NULL,
    indexer TEXT DEFAULT NULL
);

INSERT INTO file_health_old (
    id, file_path, status, last_checked, last_error, retry_count, max_retries,
    repair_retry_count, max_repair_retries, source_nzb_path, error_details,
    created_at, updated_at, release_date, scheduled_check_at, library_path,
    priority, streaming_failure_count, is_masked, metadata, indexer
)
SELECT
    id, file_path, status, last_checked, last_error, retry_count, max_retries,
    repair_retry_count, max_repair_retries, source_nzb_path, error_details,
    created_at, updated_at, release_date, scheduled_check_at, library_path,
    priority, streaming_failure_count, is_masked, metadata, indexer
FROM file_health;

DROP TABLE file_health;
ALTER TABLE file_health_old RENAME TO file_health;

CREATE INDEX idx_file_health_status ON file_health(status);
CREATE INDEX idx_file_health_path ON file_health(file_path);
CREATE INDEX idx_file_health_source ON file_health(source_nzb_path);
CREATE INDEX idx_file_health_updated ON file_health(updated_at);
CREATE INDEX idx_file_health_library_path ON file_health(library_path);
CREATE INDEX idx_file_health_masked ON file_health(is_masked) WHERE is_masked = TRUE;
CREATE INDEX idx_file_health_indexer ON file_health(indexer);
CREATE INDEX idx_file_health_release_date
    ON file_health(release_date)
    WHERE release_date IS NOT NULL;
CREATE INDEX idx_file_health_scheduled
    ON file_health(scheduled_check_at)
    WHERE scheduled_check_at IS NOT NULL;
CREATE INDEX idx_file_health_due
    ON file_health(priority DESC, scheduled_check_at ASC)
    WHERE scheduled_check_at IS NOT NULL;

CREATE TRIGGER update_file_health_timestamp
AFTER UPDATE ON file_health
BEGIN
    UPDATE file_health SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- +goose StatementEnd
