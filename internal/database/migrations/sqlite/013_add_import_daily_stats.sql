-- +goose Up
CREATE TABLE IF NOT EXISTS import_daily_stats (
    day DATE PRIMARY KEY,
    completed_count INTEGER DEFAULT 0,
    failed_count INTEGER DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS import_daily_stats;
