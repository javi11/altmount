-- +goose Up
-- +goose StatementBegin

-- Add the 'degraded' status: file has missing media-payload segments but is
-- still playable (short glitch / broken seeking). Degraded files stay visible
-- and streamable and do NOT trigger ARR repair.
ALTER TABLE file_health DROP CONSTRAINT IF EXISTS file_health_status_check;
ALTER TABLE file_health ADD CONSTRAINT file_health_status_check
    CHECK(status IN ('pending', 'checking', 'healthy', 'repair_triggered', 'corrupted', 'degraded'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

UPDATE file_health SET status = 'corrupted', updated_at = CURRENT_TIMESTAMP WHERE status = 'degraded';

ALTER TABLE file_health DROP CONSTRAINT IF EXISTS file_health_status_check;
ALTER TABLE file_health ADD CONSTRAINT file_health_status_check
    CHECK(status IN ('pending', 'checking', 'healthy', 'repair_triggered', 'corrupted'));

-- +goose StatementEnd
