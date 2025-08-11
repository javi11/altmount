-- Add rclone password and salt for encrypted files
ALTER TABLE nzb_files ADD COLUMN rclone_password TEXT DEFAULT NULL;
ALTER TABLE nzb_files ADD COLUMN rclone_salt TEXT DEFAULT NULL;

-- Index for fast lookup by rclone credentials
CREATE INDEX idx_nzb_files_rclone ON nzb_files(rclone_password, rclone_salt) WHERE rclone_password IS NOT NULL;