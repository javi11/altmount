-- +goose Up
-- +goose StatementBegin

CREATE INDEX IF NOT EXISTS idx_import_queue_nzbdav_id ON import_queue((metadata::jsonb)->>'nzbdav_id');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_import_queue_nzbdav_id;

-- +goose StatementEnd
