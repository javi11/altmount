-- +goose Up
-- +goose StatementBegin

-- Marks a file as needing an immediate ARR repair trigger, set by the FUSE
-- layer's pad recorder the moment a playback hole is confirmed (see
-- nzbfilesystem.padRecorder). Non-NULL = a trigger is pending; the health
-- worker picks these up in a dedicated lane that fires the ARR rescan only
-- (no CheckFile, no safety-folder move, no status change) so the file stays
-- degraded and streamable while a live stream may still be reading it. A
-- plain nullable column rather than a new health_status value: status is
-- CHECK-constrained (see migration 033), so a new enum value would need a
-- full table rebuild; this needs none.
ALTER TABLE file_health ADD COLUMN immediate_repair_requested_at DATETIME DEFAULT NULL;

CREATE INDEX idx_file_health_immediate_repair
    ON file_health(immediate_repair_requested_at)
    WHERE immediate_repair_requested_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_file_health_immediate_repair;
-- SQLite cannot drop a column without a full table rebuild; left in place on
-- down migration, matching migration 034's own documented precedent for the
-- same situation (download_id).

-- +goose StatementEnd
