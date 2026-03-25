-- +goose Up
-- +goose StatementBegin
-- Use a safe approach to add columns if they were skipped in 019
-- SQLite doesn't support 'IF NOT EXISTS' for columns, so we run them and let the app handle it
ALTER TABLE import_queue ADD COLUMN download_id TEXT DEFAULT NULL;
ALTER TABLE import_history ADD COLUMN download_id TEXT DEFAULT NULL;

CREATE INDEX IF NOT EXISTS idx_queue_download_id ON import_queue(download_id);
CREATE INDEX IF NOT EXISTS idx_history_download_id ON import_history(download_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_history_download_id;
DROP INDEX IF EXISTS idx_queue_download_id;
-- +goose StatementEnd