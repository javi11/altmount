-- +goose Up
-- +goose StatementBegin
ALTER TABLE import_queue ADD COLUMN download_id TEXT;
ALTER TABLE import_history ADD COLUMN download_id TEXT;

CREATE INDEX IF NOT EXISTS idx_queue_download_id ON import_queue(download_id);
CREATE INDEX IF NOT EXISTS idx_history_download_id ON import_history(download_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_history_download_id;
DROP INDEX IF EXISTS idx_queue_download_id;
ALTER TABLE import_history DROP COLUMN IF EXISTS download_id;
ALTER TABLE import_queue DROP COLUMN IF EXISTS download_id;
-- +goose StatementEnd