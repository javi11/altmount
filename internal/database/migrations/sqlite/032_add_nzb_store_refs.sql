-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS nzb_store_refs (
    store_path TEXT NOT NULL PRIMARY KEY,
    ref_count  INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME DEFAULT (datetime('now'))
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS nzb_store_refs;
-- +goose StatementEnd
