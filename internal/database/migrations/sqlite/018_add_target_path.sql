-- +goose Up
-- +goose StatementBegin
ALTER TABLE import_queue ADD COLUMN target_path TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite does not support DROP COLUMN; no-op
-- +goose StatementEnd
