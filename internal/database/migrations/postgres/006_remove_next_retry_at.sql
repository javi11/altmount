-- +goose Up
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_file_health_retry;
ALTER TABLE file_health DROP COLUMN next_retry_at;
CREATE INDEX IF NOT EXISTS idx_file_health_scheduled ON file_health(scheduled_check_at) WHERE scheduled_check_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE file_health ADD COLUMN next_retry_at TIMESTAMPTZ DEFAULT NULL;
CREATE INDEX idx_file_health_retry ON file_health(status, next_retry_at);
DROP INDEX IF EXISTS idx_file_health_scheduled;

-- +goose StatementEnd
