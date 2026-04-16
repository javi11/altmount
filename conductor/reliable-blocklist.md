# Implementation Plan: Reliable Blocklisting via DownloadID Capture

## Objective
Enable reliable blocklisting of corrupted releases by capturing and storing the `downloadID` from ARR webhooks at import time, and using this stored ID for blocklisting (failing) the release during streaming failures, bypassing unreliable history lookups.

## Scope & Impact
- **Database:** Update `file_health` table to store `download_id`.
- **Webhook:** Update `internal/api/arrs_handlers.go` to capture `downloadId` from incoming webhooks.
- **Health Worker:** Update repair logic to use the stored `download_id` for triggering ARR failures.

## Implementation Steps

### 1. Database Schema
- Modify `internal/database/models.go` to add `DownloadID` field to `FileHealth` struct.
- Add migration (SQL) to add `download_id` column to `file_health` table.

### 2. Webhook Handler
- Update `internal/api/arrs_handlers.go`:
  - Extract `downloadId` from `ArrsWebhookRequest`.
  - Pass this ID to `healthRepo.AddFileToHealthCheck`.

### 3. Health Worker Repair Logic
- Modify `internal/arrs/scanner/manager.go`:
  - Update `blocklistSonarrEpisodeFile` and `blocklistRadarrMovieFile` to accept an optional pre-known `downloadID`.
  - If `downloadID` is provided, use it directly (skip history search if possible, or use it to filter history).
- Update `internal/health/worker.go`:
  - Ensure `triggerFileRepair` retrieves the `downloadID` from the database record.

## Verification
- Add a test case in `internal/database/health_repository_test.go` to verify `download_id` persistence.
- Manual test: Import a file, trigger a streaming failure, and verify that AltMount calls the ARR's `FailContext` API using the correct `downloadID` even if the history record has been rotated out.
