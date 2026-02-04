-- +goose Up
-- +goose StatementBegin

-- Create index on nzbdav_id in metadata to allow efficient deduplication checks
-- This allows us to quickly check if a release has already been imported/queued
CREATE INDEX IF NOT EXISTS idx_import_queue_nzbdav_id ON import_queue(json_extract(metadata, '$.nzbdav_id'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_import_queue_nzbdav_id;

-- +goose StatementEnd
