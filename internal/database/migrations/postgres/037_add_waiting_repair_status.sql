-- +goose Up
-- Add the 'waiting_repair' status: an import parked while a PAR2 repair
-- rebuilds the missing articles of a damaged archive set. The item holds no
-- worker slot; the repair service returns it to 'pending' on success or to
-- 'failed' when the damage proves unrepairable.
--
-- PostgreSQL can replace the CHECK in place, no table rebuild needed. The
-- constraint name is the one PostgreSQL derives for an inline column CHECK.
ALTER TABLE import_queue DROP CONSTRAINT IF EXISTS import_queue_status_check;
ALTER TABLE import_queue ADD CONSTRAINT import_queue_status_check
    CHECK (status IN ('pending', 'processing', 'completed', 'failed', 'fallback', 'waiting_repair'));

-- +goose Down
UPDATE import_queue SET status = 'failed' WHERE status = 'waiting_repair';
ALTER TABLE import_queue DROP CONSTRAINT IF EXISTS import_queue_status_check;
ALTER TABLE import_queue ADD CONSTRAINT import_queue_status_check
    CHECK (status IN ('pending', 'processing', 'completed', 'failed', 'fallback'));
