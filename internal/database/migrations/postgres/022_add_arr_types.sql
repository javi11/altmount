-- +goose Up
-- Migration 022: Update media_files instance_type check constraint for Postgres
-- Postgres allows dropping and adding constraints

ALTER TABLE media_files DROP CONSTRAINT IF EXISTS media_files_instance_type_check;
ALTER TABLE media_files ADD CONSTRAINT media_files_instance_type_check CHECK (instance_type IN ('radarr', 'sonarr', 'lidarr', 'readarr', 'whisparr'));

-- +goose Down
ALTER TABLE media_files DROP CONSTRAINT IF EXISTS media_files_instance_type_check;
ALTER TABLE media_files ADD CONSTRAINT media_files_instance_type_check CHECK (instance_type IN ('radarr', 'sonarr'));
