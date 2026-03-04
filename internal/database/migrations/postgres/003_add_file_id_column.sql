-- +goose Up
-- +goose StatementBegin

ALTER TABLE media_files ADD COLUMN file_id INTEGER;
CREATE INDEX idx_media_files_file_id ON media_files(file_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_media_files_file_id;
ALTER TABLE media_files DROP COLUMN file_id;

-- +goose StatementEnd
