-- +goose Up
ALTER TABLE file_health ADD COLUMN download_id TEXT DEFAULT NULL;

CREATE INDEX idx_file_health_download_id ON file_health(download_id);

UPDATE file_health
SET download_id = (
    SELECT download_id 
    FROM import_history 
    WHERE TRIM(file_health.file_path, '/') LIKE TRIM(import_history.virtual_path, '/') || '%'
    LIMIT 1
)
WHERE download_id IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_file_health_download_id;
-- SQLite does not support dropping columns easily, but we can do it if needed.
-- However, standard SQLite migrations in AltMount leave columns alone in Down migrations or recreate the table.
-- Since this is a simple addition, we can just drop the index.
