-- +goose Up
-- +goose StatementBegin

-- Add Postie tracking columns to import_queue
ALTER TABLE import_queue ADD COLUMN postie_upload_id TEXT DEFAULT NULL;
ALTER TABLE import_queue ADD COLUMN postie_upload_status TEXT DEFAULT NULL CHECK(postie_upload_status IN ('pending', 'uploading', 'completed', 'failed'));
ALTER TABLE import_queue ADD COLUMN postie_uploaded_at DATETIME DEFAULT NULL;
ALTER TABLE import_queue ADD COLUMN original_release_name TEXT DEFAULT NULL;

-- Create indexes for efficient Postie queries
CREATE INDEX idx_queue_postie_status ON import_queue(postie_upload_status) WHERE postie_upload_status IS NOT NULL;
CREATE INDEX idx_queue_postie_pending ON import_queue(postie_upload_status, created_at) WHERE postie_upload_status = 'pending';
CREATE INDEX idx_queue_original_release ON import_queue(original_release_name) WHERE original_release_name IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop Postie indexes
DROP INDEX IF EXISTS idx_queue_original_release;
DROP INDEX IF EXISTS idx_queue_postie_pending;
DROP INDEX IF EXISTS idx_queue_postie_status;

-- Drop Postie columns
ALTER TABLE import_queue DROP COLUMN IF EXISTS original_release_name;
ALTER TABLE import_queue DROP COLUMN IF EXISTS postie_uploaded_at;
ALTER TABLE import_queue DROP COLUMN IF EXISTS postie_upload_status;
ALTER TABLE import_queue DROP COLUMN IF EXISTS postie_upload_id;

-- +goose StatementEnd
