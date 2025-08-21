-- +goose Up
-- +goose StatementBegin
-- Add password_hash field for direct authentication
ALTER TABLE users ADD COLUMN password_hash TEXT;

-- Create index on email for username/email login lookups
CREATE INDEX IF NOT EXISTS idx_users_email_login ON users(email) WHERE email IS NOT NULL;

-- Update the provider field to allow 'direct' for username/password auth
-- Remove the default dev admin user since we'll handle first user logic in code
DELETE FROM users WHERE user_id = 'dev_admin';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Remove the password_hash column
ALTER TABLE users DROP COLUMN password_hash;

-- Remove the email login index
DROP INDEX IF EXISTS idx_users_email_login;

-- Restore the default dev admin user
INSERT OR IGNORE INTO users (
    user_id, email, name, avatar_url, provider, provider_id, is_admin
) VALUES (
    'dev_admin', 'admin@altmount.local', 'Admin User', '', 'dev', 'admin', TRUE
);
-- +goose StatementEnd