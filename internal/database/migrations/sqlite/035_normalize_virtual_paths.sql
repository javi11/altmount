-- +goose Up
-- +goose StatementBegin
--
-- Keep the two virtual-path columns in one representation: forward slashes and
-- no leading separators. import_history has no path uniqueness constraint, so it
-- can be normalized in place. file_health is UNIQUE(file_path), so it is rebuilt
-- through a collision-free aggregate before the old rows are replaced.
UPDATE import_history
SET virtual_path = ltrim(replace(virtual_path, char(92), '/'), '/')
WHERE virtual_path <> ltrim(replace(virtual_path, char(92), '/'), '/');

DROP TABLE IF EXISTS file_health_path_normalized;

CREATE TEMP TABLE file_health_path_normalized AS
WITH normalized AS (
    SELECT h.*, ltrim(replace(h.file_path, char(92), '/'), '/') AS canonical_path
    FROM file_health h
), paths AS (
    SELECT canonical_path
    FROM normalized
    GROUP BY canonical_path
)
SELECT
    COALESCE(
        (SELECT h.id FROM normalized h
         WHERE h.file_path = p.canonical_path
         ORDER BY h.id LIMIT 1),
        (SELECT h.id FROM normalized h
         WHERE h.canonical_path = p.canonical_path
         ORDER BY h.id LIMIT 1)
    ) AS id,
    p.canonical_path AS file_path,
    (SELECT h.library_path FROM normalized h
     WHERE h.canonical_path = p.canonical_path AND NULLIF(h.library_path, '') IS NOT NULL
     ORDER BY h.updated_at DESC, h.id DESC LIMIT 1) AS library_path,
    (SELECT CASE h.status WHEN 'partial' THEN 'corrupted' ELSE h.status END FROM normalized h
     WHERE h.canonical_path = p.canonical_path
     ORDER BY CASE h.status
         WHEN 'corrupted' THEN 6
         WHEN 'repair_triggered' THEN 5
         WHEN 'degraded' THEN 4
         WHEN 'checking' THEN 3
         WHEN 'pending' THEN 2
         WHEN 'partial' THEN 2
         WHEN 'healthy' THEN 1
         ELSE 0
     END DESC, h.updated_at DESC, h.id DESC LIMIT 1) AS status,
    (SELECT MAX(h.last_checked) FROM normalized h
     WHERE h.canonical_path = p.canonical_path) AS last_checked,
    (SELECT h.last_error FROM normalized h
     WHERE h.canonical_path = p.canonical_path AND NULLIF(h.last_error, '') IS NOT NULL
     ORDER BY h.updated_at DESC, h.id DESC LIMIT 1) AS last_error,
    (SELECT MAX(h.retry_count) FROM normalized h
     WHERE h.canonical_path = p.canonical_path) AS retry_count,
    (SELECT MAX(h.max_retries) FROM normalized h
     WHERE h.canonical_path = p.canonical_path) AS max_retries,
    (SELECT MAX(h.repair_retry_count) FROM normalized h
     WHERE h.canonical_path = p.canonical_path) AS repair_retry_count,
    (SELECT MAX(h.max_repair_retries) FROM normalized h
     WHERE h.canonical_path = p.canonical_path) AS max_repair_retries,
    (SELECT h.source_nzb_path FROM normalized h
     WHERE h.canonical_path = p.canonical_path AND NULLIF(h.source_nzb_path, '') IS NOT NULL
     ORDER BY h.updated_at DESC, h.id DESC LIMIT 1) AS source_nzb_path,
    (SELECT h.error_details FROM normalized h
     WHERE h.canonical_path = p.canonical_path AND NULLIF(h.error_details, '') IS NOT NULL
     ORDER BY h.updated_at DESC, h.id DESC LIMIT 1) AS error_details,
    (SELECT MIN(h.created_at) FROM normalized h
     WHERE h.canonical_path = p.canonical_path) AS created_at,
    (SELECT MAX(h.updated_at) FROM normalized h
     WHERE h.canonical_path = p.canonical_path) AS updated_at,
    (SELECT h.release_date FROM normalized h
     WHERE h.canonical_path = p.canonical_path AND h.release_date IS NOT NULL
     ORDER BY h.updated_at DESC, h.id DESC LIMIT 1) AS release_date,
    (SELECT MIN(h.scheduled_check_at) FROM normalized h
     WHERE h.canonical_path = p.canonical_path) AS scheduled_check_at,
    (SELECT MAX(h.priority) FROM normalized h
     WHERE h.canonical_path = p.canonical_path) AS priority,
    (SELECT MAX(h.streaming_failure_count) FROM normalized h
     WHERE h.canonical_path = p.canonical_path) AS streaming_failure_count,
    (SELECT MAX(CASE WHEN COALESCE(h.is_masked, 0) THEN 1 ELSE 0 END) FROM normalized h
     WHERE h.canonical_path = p.canonical_path) AS is_masked,
    (SELECT h.metadata FROM normalized h
     WHERE h.canonical_path = p.canonical_path AND NULLIF(h.metadata, '') IS NOT NULL
     ORDER BY h.updated_at DESC, h.id DESC LIMIT 1) AS metadata,
    (SELECT h.indexer FROM normalized h
     WHERE h.canonical_path = p.canonical_path AND NULLIF(h.indexer, '') IS NOT NULL
     ORDER BY h.updated_at DESC, h.id DESC LIMIT 1) AS indexer,
    (SELECT h.download_id FROM normalized h
     WHERE h.canonical_path = p.canonical_path AND NULLIF(h.download_id, '') IS NOT NULL
     ORDER BY h.updated_at DESC, h.id DESC LIMIT 1) AS download_id
FROM paths p;

DELETE FROM file_health;

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
FROM file_health_path_normalized;

DROP TABLE file_health_path_normalized;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Path canonicalization and collision merging cannot be losslessly reversed.
-- +goose StatementEnd
