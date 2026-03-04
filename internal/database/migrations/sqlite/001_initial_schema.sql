-- +goose Up
-- +goose StatementBegin

-- ============================================================================
-- Import Queue System - Core queue processing with batching and retry logic
-- ============================================================================
CREATE TABLE import_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    nzb_path TEXT NOT NULL,
    relative_path TEXT DEFAULT NULL,
    storage_path TEXT DEFAULT NULL,
    priority INTEGER NOT NULL DEFAULT 1, -- 1=high, 2=normal, 3=low
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'completed', 'failed', 'fallback')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME DEFAULT NULL,
    completed_at DATETIME DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3,
    error_message TEXT DEFAULT NULL,
    batch_id TEXT DEFAULT NULL, -- Optional batch identifier for group processing
    metadata TEXT DEFAULT NULL, -- JSON metadata for additional processing options
    category TEXT DEFAULT NULL, -- SABnzbd compatibility
    file_size BIGINT DEFAULT NULL, -- File size in bytes
    UNIQUE(nzb_path) -- Prevent duplicate entries for same file
);

-- Indexes for efficient queue processing
CREATE INDEX idx_queue_status_priority ON import_queue(status, priority, created_at);
CREATE INDEX idx_queue_batch_id ON import_queue(batch_id);
CREATE INDEX idx_queue_status ON import_queue(status);
CREATE INDEX idx_queue_retry ON import_queue(status, retry_count, max_retries);
CREATE INDEX idx_queue_nzb_path ON import_queue(nzb_path);
CREATE INDEX idx_import_queue_category ON import_queue(category);
CREATE INDEX idx_queue_file_size ON import_queue(file_size);

-- Queue statistics table for monitoring
CREATE TABLE queue_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    total_queued INTEGER NOT NULL DEFAULT 0,
    total_processing INTEGER NOT NULL DEFAULT 0,
    total_completed INTEGER NOT NULL DEFAULT 0,
    total_failed INTEGER NOT NULL DEFAULT 0,
    avg_processing_time_ms INTEGER DEFAULT NULL,
    last_updated DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Initialize stats table with default row
INSERT INTO queue_stats (total_queued, total_processing, total_completed, total_failed) 
VALUES (0, 0, 0, 0);

-- ============================================================================
-- File Health System - Monitor and track file integrity
-- ============================================================================
CREATE TABLE file_health (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL UNIQUE, -- Virtual file path in the filesystem
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'checking', 'healthy', 'partial', 'corrupted')),
    last_checked DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 2,
    next_retry_at DATETIME DEFAULT NULL,
    source_nzb_path TEXT DEFAULT NULL, -- Source NZB file for reference
    error_details TEXT DEFAULT NULL, -- JSON error details (missing segments, etc)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for efficient health monitoring
CREATE INDEX idx_file_health_status ON file_health(status);
CREATE INDEX idx_file_health_retry ON file_health(status, next_retry_at) WHERE status NOT IN ('healthy', 'checking');
CREATE INDEX idx_file_health_path ON file_health(file_path);
CREATE INDEX idx_file_health_source ON file_health(source_nzb_path);
CREATE INDEX idx_file_health_updated ON file_health(updated_at);

-- Trigger to update updated_at on record changes
CREATE TRIGGER update_file_health_timestamp 
AFTER UPDATE ON file_health
BEGIN
    UPDATE file_health SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- ============================================================================
-- User Management System - Authentication and authorization
-- ============================================================================
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT UNIQUE NOT NULL,           -- Unique user identifier from auth provider
    email TEXT,                             -- User email address
    name TEXT,                              -- User display name (nullable after migration 005)
    avatar_url TEXT,                        -- User avatar image URL
    provider TEXT NOT NULL,                 -- OAuth provider (github, google, dev, direct, etc.)
    provider_id TEXT,                       -- Provider-specific user ID
    is_admin BOOLEAN DEFAULT FALSE,         -- Admin privileges flag
    password_hash TEXT,                     -- Password hash for direct authentication
    api_key TEXT,                           -- API key for programmatic access
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_login DATETIME,                    -- Track last login time
    
    -- Ensure unique provider/provider_id combination
    UNIQUE(provider, provider_id)
);

-- Create indexes for efficient user lookups
CREATE INDEX idx_users_user_id ON users(user_id);
CREATE INDEX idx_users_provider ON users(provider);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_email_login ON users(email) WHERE email IS NOT NULL;
CREATE INDEX idx_users_api_key ON users(api_key);

-- ============================================================================
-- Media Files System - Track media files from scraper instances
-- ============================================================================
CREATE TABLE media_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_name TEXT NOT NULL,
    instance_type TEXT NOT NULL CHECK(instance_type IN ('radarr', 'sonarr')),
    external_id INTEGER NOT NULL,
    file_path TEXT NOT NULL,
    file_size INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index for efficient file path lookups (health correlation)
CREATE INDEX idx_media_files_file_path ON media_files(file_path);

-- Index for efficient instance-based operations (cleanup, sync)
CREATE INDEX idx_media_files_instance ON media_files(instance_name, instance_type);

-- Index for efficient upsert operations
CREATE INDEX idx_media_files_external ON media_files(instance_name, instance_type, external_id);

-- Composite index for efficient sync operations
CREATE INDEX idx_media_files_sync ON media_files(instance_name, instance_type, updated_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop media files system
DROP TABLE IF EXISTS media_files;

-- Drop user management system
DROP INDEX IF EXISTS idx_users_api_key;
DROP INDEX IF EXISTS idx_users_email_login;
DROP INDEX IF EXISTS idx_users_email;
DROP INDEX IF EXISTS idx_users_provider;
DROP INDEX IF EXISTS idx_users_user_id;
DROP TABLE IF EXISTS users;

-- Drop file health system
DROP TRIGGER IF EXISTS update_file_health_timestamp;
DROP INDEX IF EXISTS idx_file_health_updated;
DROP INDEX IF EXISTS idx_file_health_source;
DROP INDEX IF EXISTS idx_file_health_path;
DROP INDEX IF EXISTS idx_file_health_retry;
DROP INDEX IF EXISTS idx_file_health_status;
DROP TABLE IF EXISTS file_health;

-- Drop queue system
DROP INDEX IF EXISTS idx_queue_file_size;
DROP INDEX IF EXISTS idx_import_queue_category;
DROP INDEX IF EXISTS idx_queue_nzb_path;
DROP INDEX IF EXISTS idx_queue_retry;
DROP INDEX IF EXISTS idx_queue_status;
DROP INDEX IF EXISTS idx_queue_batch_id;
DROP INDEX IF EXISTS idx_queue_status_priority;
DROP TABLE IF EXISTS queue_stats;
DROP TABLE IF EXISTS import_queue;

-- +goose StatementEnd