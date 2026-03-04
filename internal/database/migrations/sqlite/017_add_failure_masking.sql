-- +goose Up
-- +goose StatementBegin
ALTER TABLE file_health ADD COLUMN streaming_failure_count INTEGER DEFAULT 0;
ALTER TABLE file_health ADD COLUMN is_masked BOOLEAN DEFAULT FALSE;

-- Index for efficient lookup of masked files
CREATE INDEX idx_file_health_masked ON file_health(is_masked) WHERE is_masked = TRUE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_file_health_masked;
ALTER TABLE file_health DROP COLUMN streaming_failure_count;
ALTER TABLE file_health DROP COLUMN is_masked;
-- +goose StatementEnd
