-- +goose Up
-- +goose StatementBegin
ALTER TABLE file_health ADD COLUMN metadata JSONB DEFAULT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE file_health DROP COLUMN metadata;
-- +goose StatementEnd