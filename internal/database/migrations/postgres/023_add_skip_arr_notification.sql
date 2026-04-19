-- +goose Up
-- +goose StatementBegin
ALTER TABLE import_queue ADD COLUMN skip_arr_notification BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE import_queue DROP COLUMN IF EXISTS skip_arr_notification;
-- +goose StatementEnd
