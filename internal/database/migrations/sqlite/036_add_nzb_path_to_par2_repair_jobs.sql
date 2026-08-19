-- +goose Up
-- NZB-mode repair jobs: a release dropped at import has no metadata to plan
-- from, so the job records the NZB path instead of a virtual file path.
ALTER TABLE par2_repair_jobs ADD COLUMN nzb_path TEXT DEFAULT NULL;

-- One active job per NZB, alongside the existing per-file uniqueness. The
-- file-path index is narrowed to exclude NZB-mode rows, whose file_path is ''.
DROP INDEX IF EXISTS idx_par2_repair_active;
CREATE UNIQUE INDEX idx_par2_repair_active ON par2_repair_jobs(file_path)
    WHERE status IN ('pending','running') AND file_path <> '';
CREATE UNIQUE INDEX idx_par2_repair_active_nzb ON par2_repair_jobs(nzb_path)
    WHERE status IN ('pending','running') AND nzb_path IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_par2_repair_active_nzb;
DROP INDEX IF EXISTS idx_par2_repair_active;
CREATE UNIQUE INDEX idx_par2_repair_active ON par2_repair_jobs(file_path)
    WHERE status IN ('pending','running');
