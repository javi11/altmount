-- +goose Up
-- +goose StatementBegin
-- Covers the per-tick GetUnhealthyFiles hot query: the WHERE filters on
-- scheduled_check_at and the result is "ORDER BY priority DESC, scheduled_check_at ASC".
-- Without a matching index PostgreSQL sorts every due row each cycle. The partial
-- predicate keeps the index small (only schedulable rows) and lets the range scan +
-- ordering + LIMIT be satisfied directly from the index.
CREATE INDEX IF NOT EXISTS idx_file_health_due
    ON file_health(priority DESC, scheduled_check_at ASC)
    WHERE scheduled_check_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_file_health_due;
-- +goose StatementEnd
