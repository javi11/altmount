-- +goose Up
ALTER TABLE import_queue ADD COLUMN target_path TEXT;

-- +goose Down
ALTER TABLE import_queue DROP COLUMN target_path;
