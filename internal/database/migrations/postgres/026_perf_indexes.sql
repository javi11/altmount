-- +goose Up
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_queue_status_updated ON import_queue(status, updated_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_queue_status_updated;
-- +goose StatementEnd
