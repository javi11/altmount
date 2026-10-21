-- +goose Up
CREATE TABLE duplicate_lookup_catalog (
    file_path TEXT PRIMARY KEY,
    original_name TEXT NOT NULL,
    file_size BIGINT NOT NULL,
    identity TEXT NOT NULL,
    fingerprint TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_duplicate_lookup_name_size ON duplicate_lookup_catalog(original_name, file_size);
-- +goose Down
DROP TABLE duplicate_lookup_catalog;
