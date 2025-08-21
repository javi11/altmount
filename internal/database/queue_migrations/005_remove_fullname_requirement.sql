-- +goose Up
-- +goose StatementBegin
-- Remove full name requirement - make name field nullable and set default
-- For existing users without names, set a default value
UPDATE users SET name = user_id WHERE name = '';
-- +goose StatementEnd

-- +goose Down  
-- +goose StatementBegin
-- This down migration doesn't restore the NOT NULL constraint
-- as it would require data validation that might fail
-- +goose StatementEnd