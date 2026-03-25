-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS provider_hourly_stats (
    hour DATETIME NOT NULL,
    provider_id TEXT NOT NULL,
    bytes_downloaded BIGINT DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (hour, provider_id)
);

CREATE INDEX idx_provider_hourly_stats_provider ON provider_hourly_stats(provider_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS provider_hourly_stats;
-- +goose StatementEnd