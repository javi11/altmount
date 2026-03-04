-- +goose Up
-- +goose StatementBegin

-- Add library_path column to file_health table
ALTER TABLE file_health ADD COLUMN library_path TEXT DEFAULT NULL;

-- Create index on library_path for efficient queries
CREATE INDEX idx_file_health_library_path ON file_health(library_path);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop the index
DROP INDEX IF EXISTS idx_file_health_library_path;

-- Remove library_path column
ALTER TABLE file_health DROP COLUMN library_path;

-- +goose StatementEnd
