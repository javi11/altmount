-- +goose Up
-- +goose StatementBegin

-- Create root directory entry (parent_id = NULL for root)
INSERT INTO virtual_files (parent_id, name, size, is_directory)
VALUES (NULL, '', 0, 1);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Delete root directory entry
DELETE FROM virtual_files WHERE name = '' AND parent_id IS NULL;
-- +goose StatementEnd