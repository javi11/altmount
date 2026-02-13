-- +goose Up
-- Create import_history table for persistent tracking of every imported file
CREATE TABLE import_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    nzb_id INTEGER, -- Link to the original queue item (if still exists)
    nzb_name TEXT NOT NULL,
    file_name TEXT NOT NULL,
    file_size BIGINT,
    virtual_path TEXT NOT NULL,
    category TEXT,
    completed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    
    -- Index for fast sorting of recent items
    FOREIGN KEY(nzb_id) REFERENCES import_queue(id) ON DELETE SET NULL
);

CREATE INDEX idx_import_history_completed ON import_history(completed_at DESC);
CREATE INDEX idx_import_history_file_name ON import_history(file_name);

-- +goose Down
DROP TABLE import_history;
