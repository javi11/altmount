-- +goose Up
-- Add metadata column to import_history table
ALTER TABLE import_history ADD COLUMN metadata TEXT;

-- +goose Down
-- Remove metadata column from import_history table (SQLite doesn't support DROP COLUMN easily before 3.35.0)
-- ALTER TABLE import_history DROP COLUMN metadata;
