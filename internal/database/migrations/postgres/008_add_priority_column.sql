-- +goose Up
ALTER TABLE file_health ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE file_health DROP COLUMN priority;
