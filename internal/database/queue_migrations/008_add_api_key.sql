-- +goose Up
-- +goose StatementBegin
-- Add API key field to users table
ALTER TABLE users ADD COLUMN api_key TEXT;

-- Create index on api_key for fast lookups (when implementing authentication)
CREATE INDEX idx_users_api_key ON users(api_key);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Remove api_key field and index
DROP INDEX idx_users_api_key;
ALTER TABLE users DROP COLUMN api_key;
-- +goose StatementEnd