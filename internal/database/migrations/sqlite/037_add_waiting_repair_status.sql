-- +goose Up
-- +goose StatementBegin

-- Add the 'waiting_repair' status: an import parked while a PAR2 repair
-- rebuilds the missing articles of a damaged archive set. The item holds no
-- worker slot; the repair service returns it to 'pending' on success or to
-- 'failed' when the damage proves unrepairable.
--
-- SQLite CHECK constraints are immutable, so the table is rebuilt with the
-- widened constraint. The column list and indexes below replicate the exact
-- live schema produced by migrations 001-036.
CREATE TABLE import_queue_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    nzb_path TEXT NOT NULL,
    relative_path TEXT DEFAULT NULL,
    storage_path TEXT DEFAULT NULL,
    priority INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'completed', 'failed', 'fallback', 'waiting_repair')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME DEFAULT NULL,
    completed_at DATETIME DEFAULT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3,
    error_message TEXT DEFAULT NULL,
    batch_id TEXT DEFAULT NULL,
    metadata TEXT DEFAULT NULL,
    category TEXT DEFAULT NULL,
    file_size BIGINT DEFAULT NULL,
    target_path TEXT,
    download_id TEXT DEFAULT NULL,
    skip_arr_notification BOOLEAN NOT NULL DEFAULT FALSE,
    skip_post_import_links BOOLEAN NOT NULL DEFAULT FALSE,
    indexer TEXT DEFAULT NULL,
    UNIQUE(nzb_path)
);

INSERT INTO import_queue_new (
    id, nzb_path, relative_path, storage_path, priority, status, created_at, updated_at,
    started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata,
    category, file_size, target_path, download_id, skip_arr_notification,
    skip_post_import_links, indexer
)
SELECT
    id, nzb_path, relative_path, storage_path, priority, status, created_at, updated_at,
    started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata,
    category, file_size, target_path, download_id, skip_arr_notification,
    skip_post_import_links, indexer
FROM import_queue;

DROP TABLE import_queue;
ALTER TABLE import_queue_new RENAME TO import_queue;

CREATE INDEX idx_queue_status_priority ON import_queue(status, priority, created_at);
CREATE INDEX idx_queue_batch_id ON import_queue(batch_id);
CREATE INDEX idx_queue_status ON import_queue(status);
CREATE INDEX idx_queue_retry ON import_queue(status, retry_count, max_retries);
CREATE INDEX idx_queue_nzb_path ON import_queue(nzb_path);
CREATE INDEX idx_import_queue_category ON import_queue(category);
CREATE INDEX idx_queue_file_size ON import_queue(file_size);
CREATE INDEX idx_import_queue_nzbdav_id ON import_queue(json_extract(metadata, '$.nzbdav_id'));
CREATE INDEX idx_queue_download_id ON import_queue(download_id);
CREATE INDEX idx_queue_status_updated ON import_queue(status, updated_at);
CREATE INDEX idx_import_queue_indexer ON import_queue(indexer);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE import_queue SET status = 'failed' WHERE status = 'waiting_repair';
-- +goose StatementEnd
