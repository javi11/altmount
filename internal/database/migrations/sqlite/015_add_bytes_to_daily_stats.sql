-- +goose Up
ALTER TABLE import_daily_stats ADD COLUMN bytes_downloaded BIGINT DEFAULT 0;

-- +goose Down
-- SQLite doesn't support dropping columns easily before 3.35.0, 
-- but we can just leave it or recreate the table if needed.
-- For simplicity in this project context, we'll just not revert it or use a complex migration.
ALTER TABLE import_daily_stats DROP COLUMN bytes_downloaded;
