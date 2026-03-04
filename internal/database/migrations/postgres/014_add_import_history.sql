-- +goose Up
-- +goose StatementBegin

CREATE TABLE import_history (
    id BIGSERIAL PRIMARY KEY,
    nzb_id BIGINT,
    nzb_name TEXT NOT NULL,
    file_name TEXT NOT NULL,
    file_size BIGINT,
    virtual_path TEXT NOT NULL,
    category TEXT,
    completed_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(nzb_id) REFERENCES import_queue(id) ON DELETE SET NULL
);

CREATE INDEX idx_import_history_completed ON import_history(completed_at DESC);
CREATE INDEX idx_import_history_file_name ON import_history(file_name);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS import_history;
-- +goose StatementEnd
