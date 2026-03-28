-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='import_queue' AND column_name='download_id') THEN
        ALTER TABLE import_queue ADD COLUMN download_id TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='import_history' AND column_name='download_id') THEN
        ALTER TABLE import_history ADD COLUMN download_id TEXT;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_queue_download_id ON import_queue(download_id);
CREATE INDEX IF NOT EXISTS idx_history_download_id ON import_history(download_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_history_download_id;
DROP INDEX IF EXISTS idx_queue_download_id;
-- +goose StatementEnd