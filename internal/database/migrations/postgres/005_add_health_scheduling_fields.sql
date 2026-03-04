-- +goose Up
-- +goose StatementBegin

ALTER TABLE file_health ADD COLUMN release_date TIMESTAMPTZ;
ALTER TABLE file_health ADD COLUMN scheduled_check_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_file_health_scheduled
    ON file_health(scheduled_check_at)
    WHERE scheduled_check_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_file_health_release_date
    ON file_health(release_date)
    WHERE release_date IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_file_health_scheduled;
DROP INDEX IF EXISTS idx_file_health_release_date;
ALTER TABLE file_health DROP COLUMN scheduled_check_at;
ALTER TABLE file_health DROP COLUMN release_date;

-- +goose StatementEnd
