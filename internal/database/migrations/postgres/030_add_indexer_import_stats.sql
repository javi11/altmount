-- +goose Up
-- Create indexer_import_stats table to log the outcome of every import persistently
CREATE TABLE indexer_import_stats (
    id SERIAL PRIMARY KEY,
    indexer VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL CHECK(status IN ('success', 'failed')),
    error_message TEXT DEFAULT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_indexer_stats_name ON indexer_import_stats(indexer);
CREATE INDEX idx_indexer_stats_created ON indexer_import_stats(created_at);

-- Add indexer columns to existing tables to track indexers for active items
ALTER TABLE import_queue ADD COLUMN indexer VARCHAR(255) DEFAULT NULL;
ALTER TABLE import_history ADD COLUMN indexer VARCHAR(255) DEFAULT NULL;
ALTER TABLE file_health ADD COLUMN indexer VARCHAR(255) DEFAULT NULL;

CREATE INDEX idx_import_queue_indexer ON import_queue(indexer);
CREATE INDEX idx_import_history_indexer ON import_history(indexer);
CREATE INDEX idx_file_health_indexer ON file_health(indexer);

-- +goose Down
DROP INDEX IF EXISTS idx_file_health_indexer;
DROP INDEX IF EXISTS idx_import_history_indexer;
DROP INDEX IF EXISTS idx_import_queue_indexer;

ALTER TABLE file_health DROP COLUMN IF EXISTS indexer;
ALTER TABLE import_history DROP COLUMN IF EXISTS indexer;
ALTER TABLE import_queue DROP COLUMN IF EXISTS indexer;

DROP INDEX IF EXISTS idx_indexer_stats_created;
DROP INDEX IF EXISTS idx_indexer_stats_name;
DROP TABLE IF EXISTS indexer_import_stats;
