-- +goose Up
-- Record when a repair attempt started running and when it finished, so the UI
-- can report how long a repair took. A repair streams the entire release, so
-- its duration is the main cost the user is trading for the recovered bytes.
--
-- started_at is stamped on every claim, not only the first: a retry re-runs the
-- whole sweep, so the elapsed time shown must be the current attempt's.
ALTER TABLE par2_repair_jobs ADD COLUMN IF NOT EXISTS started_at TIMESTAMP DEFAULT NULL;
ALTER TABLE par2_repair_jobs ADD COLUMN IF NOT EXISTS finished_at TIMESTAMP DEFAULT NULL;

-- +goose Down
ALTER TABLE par2_repair_jobs DROP COLUMN IF EXISTS started_at;
ALTER TABLE par2_repair_jobs DROP COLUMN IF EXISTS finished_at;
