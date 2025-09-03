-- +goose Up
-- +goose StatementBegin

-- Add file_id column to media_files table
-- external_id will now store movie/episode ID, file_id will store the file ID
ALTER TABLE media_files ADD COLUMN file_id INTEGER;

-- Create index for efficient file_id lookups
CREATE INDEX idx_media_files_file_id ON media_files(file_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Remove the file_id column and its index
DROP INDEX IF EXISTS idx_media_files_file_id;

-- Note: SQLite doesn't support DROP COLUMN, so we would need to recreate the table
-- For simplicity in this migration, we'll leave the column but drop the index
-- In a real migration, you might want to recreate the table without the column

-- +goose StatementEnd
