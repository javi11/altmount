-- +goose Up
ALTER TABLE import_daily_stats ADD COLUMN bytes_downloaded BIGINT DEFAULT 0;

-- +goose Down
ALTER TABLE import_daily_stats DROP COLUMN bytes_downloaded;
