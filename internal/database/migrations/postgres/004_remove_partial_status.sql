-- +goose Up
-- +goose StatementBegin

UPDATE file_health SET status = 'corrupted', updated_at = CURRENT_TIMESTAMP WHERE status = 'partial';

ALTER TABLE file_health DROP CONSTRAINT IF EXISTS file_health_status_check;
ALTER TABLE file_health ADD CONSTRAINT file_health_status_check
    CHECK(status IN ('pending', 'checking', 'healthy', 'repair_triggered', 'corrupted'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE file_health DROP CONSTRAINT IF EXISTS file_health_status_check;
ALTER TABLE file_health ADD CONSTRAINT file_health_status_check
    CHECK(status IN ('pending', 'checking', 'healthy', 'partial', 'repair_triggered', 'corrupted'));

-- +goose StatementEnd
