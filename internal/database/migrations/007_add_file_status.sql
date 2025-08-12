-- +goose Up
-- +goose StatementBegin
-- Add status field to virtual_files table for tracking file availability
ALTER TABLE virtual_files ADD COLUMN status TEXT NOT NULL DEFAULT 'healthy' 
    CHECK(status IN ('healthy', 'partial', 'corrupted'));

-- Create index for status queries
CREATE INDEX idx_virtual_files_status ON virtual_files(status);

-- Update existing files to 'healthy' status (they are assumed working if they exist)
UPDATE virtual_files SET status = 'healthy' WHERE status IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop index
DROP INDEX IF EXISTS idx_virtual_files_status;

-- SQLite cannot drop columns; this down migration will leave the column in place.
-- The column will remain with default value 'healthy'
-- +goose StatementEnd