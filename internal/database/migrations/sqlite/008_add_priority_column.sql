-- +goose Up
-- Add priority column to file_health table
ALTER TABLE file_health ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- Remove priority column from file_health table
ALTER TABLE file_health DROP COLUMN priority;
