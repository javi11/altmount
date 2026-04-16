-- +goose Up
-- +goose StatementBegin
ALTER TABLE file_health ADD COLUMN download_id TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE file_health DROP COLUMN download_id;
-- +goose StatementEnd
