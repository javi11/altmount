-- +goose Up
ALTER TABLE file_health ADD COLUMN download_id TEXT DEFAULT NULL;

CREATE INDEX idx_file_health_download_id ON file_health(download_id);

-- Temporary expression index so the backfill can probe import_history by its
-- normalized (slash-trimmed) path via an index lookup instead of full-scanning
-- the table once per file_health row. Without it the correlated subquery below
-- is O(N*M) and silently hangs startup on large deployments. The index is only
-- useful for this one-time backfill, so it is dropped afterwards.
CREATE INDEX idx_import_history_trim_vpath ON import_history(TRIM(virtual_path, '/'));

UPDATE file_health
SET download_id = (
    SELECT download_id
    FROM import_history
    WHERE TRIM(import_history.virtual_path, '/') = TRIM(file_health.file_path, '/')
    LIMIT 1
)
WHERE download_id IS NULL;

DROP INDEX idx_import_history_trim_vpath;

-- +goose Down
DROP INDEX IF EXISTS idx_file_health_download_id;
-- SQLite does not support dropping columns easily, but we can do it if needed.
-- However, standard SQLite migrations in AltMount leave columns alone in Down migrations or recreate the table.
-- Since this is a simple addition, we can just drop the index.
