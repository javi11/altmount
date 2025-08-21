-- +goose Up
-- +goose StatementBegin
-- Create users table for authentication
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT UNIQUE NOT NULL,           -- Unique user identifier from auth provider
    email TEXT,                             -- User email address
    name TEXT NOT NULL,                     -- User display name
    avatar_url TEXT,                        -- User avatar image URL
    provider TEXT NOT NULL,                 -- OAuth provider (github, google, dev, etc.)
    provider_id TEXT,                       -- Provider-specific user ID
    is_admin BOOLEAN DEFAULT FALSE,         -- Admin privileges flag
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_login DATETIME,                    -- Track last login time
    
    -- Ensure unique provider/provider_id combination
    UNIQUE(provider, provider_id)
);

-- Create index on user_id for fast lookups
CREATE INDEX idx_users_user_id ON users(user_id);

-- Create index on provider for provider-specific queries
CREATE INDEX idx_users_provider ON users(provider);

-- Create index on email for email lookups
CREATE INDEX idx_users_email ON users(email);

-- Insert a default admin user for development (dev provider)
INSERT OR IGNORE INTO users (
    user_id, email, name, avatar_url, provider, provider_id, is_admin
) VALUES (
    'dev_admin', 'admin@altmount.local', 'Admin User', '', 'dev', 'admin', TRUE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_users_email;
DROP INDEX idx_users_provider;
DROP INDEX idx_users_user_id;
DROP TABLE users;
-- +goose StatementEnd