-- +goose Up
-- +goose StatementBegin
-- Add encryption field to virtual_files table
ALTER TABLE virtual_files ADD COLUMN encryption TEXT DEFAULT NULL;

-- Create index for efficient lookups by encryption type
CREATE INDEX idx_virtual_files_encryption ON virtual_files(encryption);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Remove the encryption field and its index
DROP INDEX IF EXISTS idx_virtual_files_encryption;
ALTER TABLE virtual_files DROP COLUMN encryption;
-- +goose StatementEnd