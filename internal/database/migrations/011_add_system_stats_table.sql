-- +goose Up
-- +goose StatementBegin
CREATE TABLE system_stats (
    key TEXT PRIMARY KEY,
    value BIGINT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO system_stats (key, value) VALUES ('bytes_downloaded', 0);
INSERT INTO system_stats (key, value) VALUES ('articles_downloaded', 0);
INSERT INTO system_stats (key, value) VALUES ('max_download_speed', 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS system_stats;
-- +goose StatementEnd
