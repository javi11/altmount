-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='file_health' AND column_name='metadata') THEN
        ALTER TABLE file_health ADD COLUMN metadata TEXT DEFAULT NULL;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE file_health DROP COLUMN IF EXISTS metadata;
-- +goose StatementEnd