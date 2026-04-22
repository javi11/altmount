-- +goose Up
-- +goose StatementBegin
ALTER TABLE import_queue ADD COLUMN skip_post_import_links BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE import_queue DROP COLUMN IF EXISTS skip_post_import_links;
-- +goose StatementEnd
