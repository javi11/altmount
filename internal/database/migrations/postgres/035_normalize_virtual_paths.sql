-- +goose Up
-- +goose StatementBegin
--
-- Keep the two virtual-path columns in one representation: forward slashes and
-- no leading/trailing separators. import_history has no path uniqueness constraint, so it
-- can be normalized in place. file_health is UNIQUE(file_path), so only dirty rows
-- and rows in canonical-path collision groups are rebuilt through an aggregate.
UPDATE import_history
SET virtual_path = btrim(replace(virtual_path, chr(92), '/'), '/')
WHERE virtual_path <> btrim(replace(virtual_path, chr(92), '/'), '/');

DROP TABLE IF EXISTS file_health_path_collisions;
DROP TABLE IF EXISTS file_health_path_affected;
DROP TABLE IF EXISTS file_health_path_merged;

-- Find collision keys without copying the healthy catalog. The affected table
-- below contains full rows only for dirty paths or collision groups; a clean
-- catalog therefore has no file_health DELETE/INSERT work at all.
CREATE TEMP TABLE file_health_path_collisions AS
SELECT btrim(replace(file_path, chr(92), '/'), '/') AS canonical_path
FROM file_health
GROUP BY btrim(replace(file_path, chr(92), '/'), '/')
HAVING COUNT(*) > 1;
CREATE INDEX file_health_path_collisions_path_idx
    ON file_health_path_collisions(canonical_path);

CREATE TEMP TABLE file_health_path_affected AS
SELECT h.*, btrim(replace(h.file_path, chr(92), '/'), '/') AS canonical_path
FROM file_health h
WHERE h.file_path <> btrim(replace(h.file_path, chr(92), '/'), '/')
   OR EXISTS (
       SELECT 1
       FROM file_health_path_collisions c
       WHERE c.canonical_path = btrim(replace(h.file_path, chr(92), '/'), '/')
   );

CREATE TEMP TABLE file_health_path_merged AS
WITH ranked AS (
    SELECT h.*,
        ROW_NUMBER() OVER (
            PARTITION BY canonical_path
            ORDER BY CASE status
                WHEN 'corrupted' THEN 6
                WHEN 'repair_triggered' THEN 5
                WHEN 'degraded' THEN 4
                WHEN 'checking' THEN 3
                WHEN 'pending' THEN 2
                WHEN 'partial' THEN 2
                WHEN 'healthy' THEN 1
                ELSE 0
            END DESC, updated_at DESC, id DESC) AS status_rank,
        ROW_NUMBER() OVER (
            PARTITION BY canonical_path
            ORDER BY CASE WHEN NULLIF(library_path, '') IS NOT NULL THEN 0 ELSE 1 END,
                updated_at DESC, id DESC) AS library_rank,
        ROW_NUMBER() OVER (
            PARTITION BY canonical_path
            ORDER BY CASE WHEN NULLIF(last_error, '') IS NOT NULL THEN 0 ELSE 1 END,
                updated_at DESC, id DESC) AS last_error_rank,
        ROW_NUMBER() OVER (
            PARTITION BY canonical_path
            ORDER BY CASE WHEN NULLIF(source_nzb_path, '') IS NOT NULL THEN 0 ELSE 1 END,
                updated_at DESC, id DESC) AS source_nzb_rank,
        ROW_NUMBER() OVER (
            PARTITION BY canonical_path
            ORDER BY CASE WHEN NULLIF(error_details, '') IS NOT NULL THEN 0 ELSE 1 END,
                updated_at DESC, id DESC) AS error_details_rank,
        ROW_NUMBER() OVER (
            PARTITION BY canonical_path
            ORDER BY CASE WHEN release_date IS NOT NULL THEN 0 ELSE 1 END,
                updated_at DESC, id DESC) AS release_date_rank,
        ROW_NUMBER() OVER (
            PARTITION BY canonical_path
            ORDER BY CASE WHEN metadata IS NOT NULL THEN 0 ELSE 1 END,
                updated_at DESC, id DESC) AS metadata_rank,
        ROW_NUMBER() OVER (
            PARTITION BY canonical_path
            ORDER BY CASE WHEN NULLIF(indexer, '') IS NOT NULL THEN 0 ELSE 1 END,
                updated_at DESC, id DESC) AS indexer_rank,
        ROW_NUMBER() OVER (
            PARTITION BY canonical_path
            ORDER BY CASE WHEN NULLIF(download_id, '') IS NOT NULL THEN 0 ELSE 1 END,
                updated_at DESC, id DESC) AS download_id_rank
    FROM file_health_path_affected h
)
SELECT
    COALESCE(MIN(CASE WHEN file_path = canonical_path THEN id END), MIN(id)) AS id,
    canonical_path AS file_path,
    MAX(CASE WHEN library_rank = 1 AND NULLIF(library_path, '') IS NOT NULL THEN library_path END) AS library_path,
    MAX(CASE WHEN status_rank = 1 THEN CASE status WHEN 'partial' THEN 'corrupted' ELSE status END END) AS status,
    MAX(last_checked) AS last_checked,
    MAX(CASE WHEN last_error_rank = 1 AND NULLIF(last_error, '') IS NOT NULL THEN last_error END) AS last_error,
    MAX(retry_count) AS retry_count,
    MAX(max_retries) AS max_retries,
    MAX(repair_retry_count) AS repair_retry_count,
    MAX(max_repair_retries) AS max_repair_retries,
    MAX(CASE WHEN source_nzb_rank = 1 AND NULLIF(source_nzb_path, '') IS NOT NULL THEN source_nzb_path END) AS source_nzb_path,
    MAX(CASE WHEN error_details_rank = 1 AND NULLIF(error_details, '') IS NOT NULL THEN error_details END) AS error_details,
    MIN(created_at) AS created_at,
    MAX(updated_at) AS updated_at,
    MAX(CASE WHEN release_date_rank = 1 AND release_date IS NOT NULL THEN release_date END) AS release_date,
    MIN(scheduled_check_at) AS scheduled_check_at,
    MAX(priority) AS priority,
    MAX(streaming_failure_count) AS streaming_failure_count,
    BOOL_OR(COALESCE(is_masked, FALSE)) AS is_masked,
    (array_agg(metadata ORDER BY metadata_rank) FILTER (WHERE metadata IS NOT NULL))[1] AS metadata,
    MAX(CASE WHEN indexer_rank = 1 AND NULLIF(indexer, '') IS NOT NULL THEN indexer END) AS indexer,
    MAX(CASE WHEN download_id_rank = 1 AND NULLIF(download_id, '') IS NOT NULL THEN download_id END) AS download_id
FROM ranked
GROUP BY canonical_path;

-- Remove only rows represented by the affected aggregate. Clean, canonical
-- rows retain their IDs and all column values byte-for-byte.
DELETE FROM file_health
WHERE id IN (SELECT id FROM file_health_path_affected);

INSERT INTO file_health (
    id, file_path, library_path, status, last_checked, last_error,
    retry_count, max_retries, repair_retry_count, max_repair_retries,
    source_nzb_path, error_details, created_at, updated_at, release_date,
    scheduled_check_at, priority, streaming_failure_count, is_masked, metadata,
    indexer, download_id
)
SELECT
    id, file_path, library_path, status, last_checked, last_error,
    retry_count, max_retries, repair_retry_count, max_repair_retries,
    source_nzb_path, error_details, created_at, updated_at, release_date,
    scheduled_check_at, priority, streaming_failure_count, is_masked, metadata,
    indexer, download_id
FROM file_health_path_merged;

SELECT setval(
    pg_get_serial_sequence('file_health', 'id'),
    COALESCE((SELECT MAX(id) FROM file_health), 1),
    (SELECT COUNT(*) > 0 FROM file_health)
);

DROP TABLE file_health_path_merged;
DROP TABLE file_health_path_affected;
DROP TABLE file_health_path_collisions;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Path canonicalization and collision merging cannot be losslessly reversed.
-- +goose StatementEnd
