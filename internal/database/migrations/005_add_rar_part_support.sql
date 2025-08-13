-- +goose Up
-- +goose StatementBegin

-- Add RAR part management fields to nzb_files table
ALTER TABLE nzb_files ADD COLUMN parent_nzb_id INTEGER DEFAULT NULL;
ALTER TABLE nzb_files ADD COLUMN part_type TEXT NOT NULL DEFAULT 'main' CHECK(part_type IN ('main', 'rar_part', 'par2'));
ALTER TABLE nzb_files ADD COLUMN archive_name TEXT DEFAULT NULL;

-- Add foreign key constraint for parent_nzb_id (references nzb_files.id)
-- Note: SQLite doesn't support adding foreign key constraints to existing tables
-- So we'll rely on application-level integrity for now

-- Add RAR streaming support fields to rar_contents table
ALTER TABLE rar_contents ADD COLUMN file_offset INTEGER DEFAULT NULL;
ALTER TABLE rar_contents ADD COLUMN rar_part_index INTEGER DEFAULT NULL;  
ALTER TABLE rar_contents ADD COLUMN is_directory BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE rar_contents ADD COLUMN mod_time DATETIME DEFAULT NULL;

-- Create indexes for efficient RAR part queries
CREATE INDEX idx_nzb_files_parent_nzb_id ON nzb_files(parent_nzb_id);
CREATE INDEX idx_nzb_files_part_type ON nzb_files(part_type);
CREATE INDEX idx_nzb_files_archive_name ON nzb_files(archive_name);
CREATE INDEX idx_nzb_files_parent_part_type ON nzb_files(parent_nzb_id, part_type);
CREATE INDEX idx_nzb_files_parent_archive ON nzb_files(parent_nzb_id, archive_name);

-- Create indexes for RAR contents
CREATE INDEX idx_rar_contents_rar_part_index ON rar_contents(rar_part_index);
CREATE INDEX idx_rar_contents_is_directory ON rar_contents(is_directory);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop indexes
DROP INDEX IF EXISTS idx_rar_contents_is_directory;
DROP INDEX IF EXISTS idx_rar_contents_rar_part_index;
DROP INDEX IF EXISTS idx_nzb_files_parent_archive;
DROP INDEX IF EXISTS idx_nzb_files_parent_part_type;
DROP INDEX IF EXISTS idx_nzb_files_archive_name;
DROP INDEX IF EXISTS idx_nzb_files_part_type;
DROP INDEX IF EXISTS idx_nzb_files_parent_nzb_id;

-- Note: SQLite doesn't support dropping columns easily
-- The columns will remain but won't be used
-- In a real production environment, you'd need to recreate the table

-- +goose StatementEnd