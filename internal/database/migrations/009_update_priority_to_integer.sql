-- +goose Up
-- +goose StatementBegin

-- Ensure priority column is INTEGER (if it was BOOLEAN it's already INTEGER in SQLite, but let's be explicit in our schema)
-- SQLite BOOLEAN is just an alias for INTEGER, so 0/1 values are already there.
-- We just want to ensure our constants (0, 1, 2) work as expected.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- +goose StatementEnd
