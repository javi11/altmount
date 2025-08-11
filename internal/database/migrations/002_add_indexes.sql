-- +goose Up
-- +goose StatementBegin
-- Indexes for performance optimization
CREATE INDEX idx_nzb_files_path ON nzb_files(path);
CREATE INDEX idx_nzb_files_filename ON nzb_files(filename);
CREATE INDEX idx_nzb_files_type ON nzb_files(nzb_type);

CREATE INDEX idx_virtual_files_nzb_id ON virtual_files(nzb_file_id);
CREATE INDEX idx_virtual_files_path ON virtual_files(virtual_path);
CREATE INDEX idx_virtual_files_parent ON virtual_files(parent_path);
CREATE INDEX idx_virtual_files_directory ON virtual_files(is_directory);

CREATE INDEX idx_rar_contents_virtual_file_id ON rar_contents(virtual_file_id);
CREATE INDEX idx_rar_contents_path ON rar_contents(internal_path);

CREATE INDEX idx_file_metadata_virtual_file_id ON file_metadata(virtual_file_id);
CREATE INDEX idx_file_metadata_key ON file_metadata(key);

ALTER TABLE nzb_files ADD COLUMN segment_size INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_nzb_files_segment_size ON nzb_files(segment_size);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_file_metadata_key;
DROP INDEX IF EXISTS idx_file_metadata_virtual_file_id;
DROP INDEX IF EXISTS idx_rar_contents_path;
DROP INDEX IF EXISTS idx_rar_contents_virtual_file_id;
DROP INDEX IF EXISTS idx_virtual_files_directory;
DROP INDEX IF EXISTS idx_virtual_files_parent;
DROP INDEX IF EXISTS idx_virtual_files_path;
DROP INDEX IF EXISTS idx_virtual_files_nzb_id;
DROP INDEX IF EXISTS idx_nzb_files_type;
DROP INDEX IF EXISTS idx_nzb_files_filename;
DROP INDEX IF EXISTS idx_nzb_files_path;
DROP INDEX IF EXISTS idx_nzb_files_segment_size;
-- SQLite cannot drop columns; this down migration will leave the column in place.
-- +goose StatementEnd