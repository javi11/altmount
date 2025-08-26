-- Add category column for SABnzbd compatibility
-- +goose Up
-- +goose StatementBegin
ALTER TABLE import_queue ADD COLUMN category TEXT;

-- Create index on category for filtering
CREATE INDEX idx_import_queue_category ON import_queue(category);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Remove category field and index
DROP INDEX idx_import_queue_category;
ALTER TABLE import_queue DROP COLUMN category;
-- +goose StatementEnd