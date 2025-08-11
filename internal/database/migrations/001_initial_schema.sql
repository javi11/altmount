-- +goose Up
-- +goose StatementBegin
CREATE TABLE nzb_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL UNIQUE,
    filename TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    nzb_type TEXT NOT NULL CHECK(nzb_type IN ('single_file', 'multi_file', 'rar_archive')),
    segments_count INTEGER NOT NULL DEFAULT 0,
    segments_data TEXT -- JSON data containing NZB segments info
);

CREATE TABLE virtual_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    nzb_file_id INTEGER,
    virtual_path TEXT NOT NULL,
    filename TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    is_directory BOOLEAN NOT NULL DEFAULT FALSE,
    parent_path TEXT,
    FOREIGN KEY (nzb_file_id) REFERENCES nzb_files(id) ON DELETE CASCADE
);

CREATE TABLE rar_contents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    virtual_file_id INTEGER NOT NULL,
    internal_path TEXT NOT NULL,
    filename TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    compressed_size INTEGER NOT NULL DEFAULT 0,
    crc32 TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (virtual_file_id) REFERENCES virtual_files(id) ON DELETE CASCADE
);

CREATE TABLE file_metadata (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    virtual_file_id INTEGER NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (virtual_file_id) REFERENCES virtual_files(id) ON DELETE CASCADE,
    UNIQUE(virtual_file_id, key)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS file_metadata;
DROP TABLE IF EXISTS rar_contents;
DROP TABLE IF EXISTS virtual_files;
DROP TABLE IF EXISTS nzb_files;
-- +goose StatementEnd