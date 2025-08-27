-- +goose Up
-- +goose StatementBegin
-- Add file_size column to import_queue table
ALTER TABLE import_queue ADD COLUMN file_size BIGINT DEFAULT NULL;

-- Add index for efficient size-based queries
CREATE INDEX idx_queue_file_size ON import_queue(file_size);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Remove the index first
DROP INDEX IF EXISTS idx_queue_file_size;

-- Remove the file_size column
ALTER TABLE import_queue DROP COLUMN file_size;
-- +goose StatementEnd