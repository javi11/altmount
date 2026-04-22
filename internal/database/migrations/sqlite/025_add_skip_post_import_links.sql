-- +goose Up
-- +goose StatementBegin
ALTER TABLE import_queue ADD COLUMN skip_post_import_links BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite does not support DROP COLUMN in older versions; intentional no-op
-- +goose StatementEnd
