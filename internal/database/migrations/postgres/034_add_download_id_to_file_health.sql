-- +goose Up
ALTER TABLE file_health ADD COLUMN download_id TEXT DEFAULT NULL;

CREATE INDEX idx_file_health_download_id ON file_health(download_id);

UPDATE file_health
SET download_id = (
    SELECT download_id 
    FROM import_history 
    WHERE TRIM(import_history.virtual_path, '/') = TRIM(file_health.file_path, '/')
    LIMIT 1
)
WHERE download_id IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_file_health_download_id;
ALTER TABLE file_health DROP COLUMN IF EXISTS download_id;
