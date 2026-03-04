-- +goose Up
-- +goose StatementBegin

ALTER TABLE file_health ADD COLUMN repair_retry_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE file_health ADD COLUMN max_repair_retries INTEGER NOT NULL DEFAULT 3;

ALTER TABLE file_health DROP CONSTRAINT IF EXISTS file_health_status_check;
ALTER TABLE file_health ADD CONSTRAINT file_health_status_check
    CHECK(status IN ('pending', 'checking', 'healthy', 'partial', 'repair_triggered', 'corrupted'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE file_health DROP CONSTRAINT IF EXISTS file_health_status_check;
ALTER TABLE file_health ADD CONSTRAINT file_health_status_check
    CHECK(status IN ('pending', 'checking', 'healthy', 'partial', 'corrupted'));

ALTER TABLE file_health DROP COLUMN repair_retry_count;
ALTER TABLE file_health DROP COLUMN max_repair_retries;

-- +goose StatementEnd
