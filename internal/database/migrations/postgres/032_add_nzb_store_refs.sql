-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS nzb_store_refs (
    store_path TEXT        NOT NULL PRIMARY KEY,
    ref_count  BIGINT      NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS nzb_store_refs;
-- +goose StatementEnd
