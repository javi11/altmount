-- +goose Up
-- +goose StatementBegin
CREATE TABLE par2_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    nzb_file_id INTEGER NOT NULL,
    filename TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    segments_data TEXT NOT NULL, -- JSON data containing NZB segments info for this par2 file
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (nzb_file_id) REFERENCES nzb_files(id) ON DELETE CASCADE
);

-- Create index for efficient lookups by nzb_file_id
CREATE INDEX idx_par2_files_nzb_file_id ON par2_files(nzb_file_id);

-- Create index for efficient lookups by filename
CREATE INDEX idx_par2_files_filename ON par2_files(filename);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_par2_files_filename;
DROP INDEX IF EXISTS idx_par2_files_nzb_file_id;
DROP TABLE IF EXISTS par2_files;
-- +goose StatementEnd