-- +goose Up
-- +goose StatementBegin
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
DROP TABLE IF EXISTS media_files;
-- +goose StatementEnd