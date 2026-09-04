-- +goose Up
-- Group repairs by release: a PAR2 repair sweeps the entire release, so every
-- damaged file of one NZB shares a single job instead of one row per file.
-- release_ref is the release's shared NzbStore ref; member_paths is the JSON
-- list of every file the job repairs on behalf of (the trigger included).
ALTER TABLE par2_repair_jobs ADD COLUMN release_ref TEXT DEFAULT NULL;
ALTER TABLE par2_repair_jobs ADD COLUMN member_paths TEXT DEFAULT NULL;
CREATE UNIQUE INDEX idx_par2_repair_active_release ON par2_repair_jobs(release_ref)
	WHERE status IN ('pending','running') AND release_ref IS NOT NULL;

-- +goose Down
DROP INDEX idx_par2_repair_active_release;
ALTER TABLE par2_repair_jobs DROP COLUMN release_ref;
ALTER TABLE par2_repair_jobs DROP COLUMN member_paths;
