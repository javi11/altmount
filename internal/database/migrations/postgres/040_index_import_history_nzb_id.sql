-- +goose Up
-- +goose StatementBegin
-- Deleting a queue item now also deletes its import_history copy, so the
-- SABnzbd history view cannot resurrect a job the user removed. Those deletes
-- filter on import_history.nzb_id, which had no index — every single, bulk and
-- "clear completed" delete scanned the whole history table.
CREATE INDEX IF NOT EXISTS idx_import_history_nzb_id ON import_history(nzb_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_import_history_nzb_id;
-- +goose StatementEnd
