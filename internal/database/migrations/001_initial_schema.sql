-- +goose Up
-- +goose StatementBegin
CREATE TABLE virtual_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id INTEGER,
    name TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    is_directory BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'healthy' CHECK(status IN ('healthy', 'partial', 'corrupted')),
    FOREIGN KEY (parent_id) REFERENCES virtual_files(id) ON DELETE CASCADE
);

CREATE TABLE nzb_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    segments_data TEXT, -- JSON data containing NZB segments info
    password TEXT DEFAULT NULL,
    encryption TEXT DEFAULT NULL, -- Encryption type (e.g., 'rclone', 'headers')
    salt TEXT DEFAULT NULL,
    FOREIGN KEY (id) REFERENCES virtual_files(id) ON DELETE CASCADE
);

CREATE TABLE nzb_rar_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    rar_parts TEXT NOT NULL, -- JSON data containing NZB rar parts and segments info
    FOREIGN KEY (id) REFERENCES virtual_files(id) ON DELETE CASCADE
);

CREATE TABLE par2_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    segments_data TEXT NOT NULL, -- JSON data containing NZB segments info for this par2 file
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (id) REFERENCES virtual_files(id) ON DELETE CASCADE
);

-- Create indexes for performance
CREATE INDEX idx_virtual_files_parent_id ON virtual_files(parent_id);
CREATE INDEX idx_virtual_files_name ON virtual_files(name);
CREATE INDEX idx_virtual_files_is_directory ON virtual_files(is_directory);
CREATE INDEX idx_virtual_files_status ON virtual_files(status);

CREATE INDEX idx_nzb_files_name ON nzb_files(name);
CREATE INDEX idx_nzb_rar_files_name ON nzb_rar_files(name);
CREATE INDEX idx_par2_files_name ON par2_files(name);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_par2_files_name;
DROP INDEX IF EXISTS idx_nzb_rar_files_name;
DROP INDEX IF EXISTS idx_nzb_files_name;
DROP INDEX IF EXISTS idx_virtual_files_status;
DROP INDEX IF EXISTS idx_virtual_files_is_directory;
DROP INDEX IF EXISTS idx_virtual_files_name;
DROP INDEX IF EXISTS idx_virtual_files_parent_id;

DROP TABLE IF EXISTS par2_files;
DROP TABLE IF EXISTS nzb_rar_files;
DROP TABLE IF EXISTS nzb_files;
DROP TABLE IF EXISTS virtual_files;
-- +goose StatementEnd