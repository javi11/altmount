-- +goose Up
CREATE TABLE IF NOT EXISTS import_hourly_stats (
    hour TIMESTAMPTZ PRIMARY KEY,
    completed_count INTEGER DEFAULT 0,
    failed_count INTEGER DEFAULT 0,
    bytes_downloaded INTEGER DEFAULT 0,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS import_hourly_stats;
