-- +goose Up
-- +goose StatementBegin
CREATE TABLE import_migrations (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    source         TEXT    NOT NULL,
    external_id    TEXT    NOT NULL,
    queue_item_id  INTEGER,
    relative_path  TEXT    NOT NULL DEFAULT '',
    final_path     TEXT,
    status         TEXT    NOT NULL DEFAULT 'pending',
    error          TEXT,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(source, external_id)
);
CREATE INDEX idx_import_migrations_status ON import_migrations(source, status);
CREATE INDEX idx_import_migrations_queue  ON import_migrations(queue_item_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_import_migrations_queue;
DROP INDEX IF EXISTS idx_import_migrations_status;
DROP TABLE IF EXISTS import_migrations;
-- +goose StatementEnd
