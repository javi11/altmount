-- +goose Up
ALTER TABLE import_history ADD COLUMN deleted_from_sabnzbd BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE import_history DROP COLUMN deleted_from_sabnzbd;
