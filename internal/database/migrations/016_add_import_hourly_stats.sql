-- +goose Up
CREATE TABLE IF NOT EXISTS import_hourly_stats (
    hour DATETIME PRIMARY KEY, -- Start of the hour (e.g., 2026-02-20 14:00:00)
    completed_count INTEGER DEFAULT 0,
    failed_count INTEGER DEFAULT 0,
    bytes_downloaded INTEGER DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS import_hourly_stats;
