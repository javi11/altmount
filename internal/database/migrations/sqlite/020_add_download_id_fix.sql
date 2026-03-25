-- +goose Up
-- +goose StatementBegin
-- SQLite doesn't support 'IF NOT EXISTS' for columns.
-- Since 019 already added the columns, we skip them here to avoid 'duplicate column' errors.
-- These indexes are also in 019, but we keep them with IF NOT EXISTS to be safe.
CREATE INDEX IF NOT EXISTS idx_queue_download_id ON import_queue(download_id);
CREATE INDEX IF NOT EXISTS idx_history_download_id ON import_history(download_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_history_download_id;
DROP INDEX IF EXISTS idx_queue_download_id;
-- +goose StatementEnd