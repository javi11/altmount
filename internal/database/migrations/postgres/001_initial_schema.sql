-- +goose Up
-- +goose StatementBegin

CREATE TABLE import_queue (
    id BIGSERIAL PRIMARY KEY,
    nzb_path TEXT NOT NULL,
    relative_path TEXT DEFAULT NULL,
    storage_path TEXT DEFAULT NULL,
    priority INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'completed', 'failed', 'fallback')),
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMPTZ DEFAULT NULL,
    completed_at TIMESTAMPTZ DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3,
    error_message TEXT DEFAULT NULL,
    batch_id TEXT DEFAULT NULL,
    metadata TEXT DEFAULT NULL,
    category TEXT DEFAULT NULL,
    file_size BIGINT DEFAULT NULL,
    UNIQUE(nzb_path)
);

CREATE INDEX idx_queue_status_priority ON import_queue(status, priority, created_at);
CREATE INDEX idx_queue_batch_id ON import_queue(batch_id);
CREATE INDEX idx_queue_status ON import_queue(status);
CREATE INDEX idx_queue_retry ON import_queue(status, retry_count, max_retries);
CREATE INDEX idx_queue_nzb_path ON import_queue(nzb_path);
CREATE INDEX idx_import_queue_category ON import_queue(category);
CREATE INDEX idx_queue_file_size ON import_queue(file_size);

CREATE TABLE queue_stats (
    id BIGSERIAL PRIMARY KEY,
    total_queued INTEGER NOT NULL DEFAULT 0,
    total_processing INTEGER NOT NULL DEFAULT 0,
    total_completed INTEGER NOT NULL DEFAULT 0,
    total_failed INTEGER NOT NULL DEFAULT 0,
    avg_processing_time_ms INTEGER DEFAULT NULL,
    last_updated TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO queue_stats (total_queued, total_processing, total_completed, total_failed)
VALUES (0, 0, 0, 0);

CREATE TABLE file_health (
    id BIGSERIAL PRIMARY KEY,
    file_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'checking', 'healthy', 'partial', 'corrupted')),
    last_checked TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 2,
    next_retry_at TIMESTAMPTZ DEFAULT NULL,
    source_nzb_path TEXT DEFAULT NULL,
    error_details TEXT DEFAULT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_file_health_status ON file_health(status);
CREATE INDEX idx_file_health_retry ON file_health(status, next_retry_at) WHERE status NOT IN ('healthy', 'checking');
CREATE INDEX idx_file_health_path ON file_health(file_path);
CREATE INDEX idx_file_health_source ON file_health(source_nzb_path);
CREATE INDEX idx_file_health_updated ON file_health(updated_at);

CREATE OR REPLACE FUNCTION update_file_health_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_file_health_timestamp
BEFORE UPDATE ON file_health
FOR EACH ROW EXECUTE FUNCTION update_file_health_timestamp();

CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT UNIQUE NOT NULL,
    email TEXT,
    name TEXT,
    avatar_url TEXT,
    provider TEXT NOT NULL,
    provider_id TEXT,
    is_admin BOOLEAN DEFAULT FALSE,
    password_hash TEXT,
    api_key TEXT,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    last_login TIMESTAMPTZ,
    UNIQUE(provider, provider_id)
);

CREATE INDEX idx_users_user_id ON users(user_id);
CREATE INDEX idx_users_provider ON users(provider);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_email_login ON users(email) WHERE email IS NOT NULL;
CREATE INDEX idx_users_api_key ON users(api_key);

CREATE TABLE media_files (
    id BIGSERIAL PRIMARY KEY,
    instance_name TEXT NOT NULL,
    instance_type TEXT NOT NULL CHECK(instance_type IN ('radarr', 'sonarr')),
    external_id INTEGER NOT NULL,
    file_path TEXT NOT NULL,
    file_size INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_media_files_file_path ON media_files(file_path);
CREATE INDEX idx_media_files_instance ON media_files(instance_name, instance_type);
CREATE INDEX idx_media_files_external ON media_files(instance_name, instance_type, external_id);
CREATE INDEX idx_media_files_sync ON media_files(instance_name, instance_type, updated_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS media_files;
DROP INDEX IF EXISTS idx_users_api_key;
DROP INDEX IF EXISTS idx_users_email_login;
DROP INDEX IF EXISTS idx_users_email;
DROP INDEX IF EXISTS idx_users_provider;
DROP INDEX IF EXISTS idx_users_user_id;
DROP TABLE IF EXISTS users;
DROP TRIGGER IF EXISTS update_file_health_timestamp ON file_health;
DROP FUNCTION IF EXISTS update_file_health_timestamp();
DROP INDEX IF EXISTS idx_file_health_updated;
DROP INDEX IF EXISTS idx_file_health_source;
DROP INDEX IF EXISTS idx_file_health_path;
DROP INDEX IF EXISTS idx_file_health_retry;
DROP INDEX IF EXISTS idx_file_health_status;
DROP TABLE IF EXISTS file_health;
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
