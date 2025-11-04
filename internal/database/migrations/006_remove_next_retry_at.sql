-- +goose Up
-- +goose StatementBegin
-- Remove next_retry_at field and consolidate on scheduled_check_at for all health check scheduling

-- Drop the index that references next_retry_at
DROP INDEX IF EXISTS idx_file_health_retry;

-- Drop the next_retry_at column
ALTER TABLE file_health DROP COLUMN next_retry_at;

-- Create new index on scheduled_check_at for efficient querying of files due for checks
CREATE INDEX IF NOT EXISTS idx_file_health_scheduled ON file_health(scheduled_check_at) WHERE scheduled_check_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Restore next_retry_at column
ALTER TABLE file_health ADD COLUMN next_retry_at DATETIME DEFAULT NULL;

-- Restore the original index
CREATE INDEX idx_file_health_retry ON file_health(status, next_retry_at);

-- Drop the new index
DROP INDEX IF EXISTS idx_file_health_scheduled;
-- +goose StatementEnd
