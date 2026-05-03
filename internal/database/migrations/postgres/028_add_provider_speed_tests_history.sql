-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS provider_speed_tests_history (
    id SERIAL PRIMARY KEY,
    provider_id TEXT NOT NULL,
    speed_mbps REAL NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_provider_speed_tests_history_provider_id ON provider_speed_tests_history(provider_id);
CREATE INDEX IF NOT EXISTS idx_provider_speed_tests_history_created_at ON provider_speed_tests_history(created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS provider_speed_tests_history;
-- +goose StatementEnd