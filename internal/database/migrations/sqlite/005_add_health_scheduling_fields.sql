-- +goose Up
-- +goose StatementBegin
-- Add scheduling fields for health checks
ALTER TABLE file_health ADD COLUMN release_date DATETIME;
ALTER TABLE file_health ADD COLUMN scheduled_check_at DATETIME;

-- Index for efficient scheduling queries
CREATE INDEX IF NOT EXISTS idx_file_health_scheduled
    ON file_health(scheduled_check_at)
    WHERE scheduled_check_at IS NOT NULL;

-- Index for release date lookups
CREATE INDEX IF NOT EXISTS idx_file_health_release_date
    ON file_health(release_date)
    WHERE release_date IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Note: SQLite doesn't support DROP COLUMN, so we only drop indexes
DROP INDEX IF EXISTS idx_file_health_scheduled;
DROP INDEX IF EXISTS idx_file_health_release_date;
-- +goose StatementEnd