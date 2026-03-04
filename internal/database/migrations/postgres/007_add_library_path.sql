-- +goose Up
-- +goose StatementBegin

ALTER TABLE file_health ADD COLUMN library_path TEXT DEFAULT NULL;
CREATE INDEX idx_file_health_library_path ON file_health(library_path);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_file_health_library_path;
ALTER TABLE file_health DROP COLUMN library_path;

-- +goose StatementEnd
