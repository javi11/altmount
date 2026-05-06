-- +goose Up
-- +goose StatementBegin
ALTER TABLE import_history ADD COLUMN metadata TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE import_history DROP COLUMN metadata;
-- +goose StatementEnd
