-- +goose Up
-- +goose StatementBegin

-- Create root directory entry (parent_id = NULL for root)
INSERT INTO virtual_files (nzb_file_id, parent_id, virtual_path, filename, size, is_directory)
VALUES (NULL, NULL, '/', '', 0, 1);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Delete root directory entry
DELETE FROM virtual_files WHERE virtual_path = '/' AND parent_id IS NULL;
-- +goose StatementEnd