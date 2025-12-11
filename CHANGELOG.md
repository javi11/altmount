# Changelog

## v0.6.0-alpha7 - 2025-12-09

### ✨ New Features

*   **NZBDav Database Import:**
    *   Introduced a new feature to import releases directly from an existing NZBDav SQLite database.
    *   **UI Integration:** A new "Import" page in the UI allows users to upload their `db.sqlite` file.
    *   **Customizable Output:** Users can specify a target "Root Folder Name" (e.g., "MyLibrary"). Imported movies will be placed under `[RootFolder]/movies` and TV series under `[RootFolder]/tv`.
    *   **Automatic Categorization:** The importer automatically categorizes releases as "movies" or "tv" based on release names and paths found in the NZBDav database.
    *   **Queue Integration:** All imported releases are added to the existing `altmount` import queue for processing, ensuring proper metadata generation and file handling.

### ⚡ Improvements & Logic Changes

*   **Smarter Repair Workflow:**
    *   *Previously:* Triggering a repair would delete the health record, hiding the file from the list until it was re-imported (or failed again).
    *   *Now:* Triggering a repair sets the status to `repair_triggered` (Visualized as a **Repairing** badge/wrench icon).
    *   **Delayed Re-check:** The system now waits **1 hour** before re-checking a file in "Repairing" state, giving Sonarr/Radarr ample time to grab a new release without triggering false positives.

*   **Exponential Backoff for Retries:**
    *   Failed health checks (due to provider issues) no longer retry immediately in a tight loop.
    *   Implemented backoff strategy: Wait 15m -> 30m -> 60m before retrying. This reduces API spam and saves resources during temporary provider outages.

*   **Robust ARR Path Matching:**
    *   Enhanced the logic for finding files in Sonarr/Radarr.
    *   Added a fallback strategy: If exact path lookup fails, it attempts to match based on filename `SxxEyy` parsing or relative path suffixes. This fixes issues where `altmount` couldn't trigger repairs for files with slightly different mount paths.

*   **UI Visual Overhaul:**
    *   **Dynamic Icons:** Replaced static heart icons with dynamic status indicators (Green Heart = Healthy, Red Broken Heart = Corrupted, Spinning Wrench = Repairing, Spinning Loader = Checking, Clock = Pending).
    *   **Priority Support:** Added a "Priority" column (High/Normal) to the table to surface urgent tasks.

### 🐛 Bug Fixes & Stability

*   **Panic Recovery:** Added `recover()` to background workers to prevent the entire server from crashing if a single health check panics.
*   **Database Migration:** Fixed SQLite compatibility issues in the migration script for the new `priority` column.
*   **Persistence:** Fixed an issue where `max_concurrent_jobs` setting was not persisting across restarts.

### 🏗️ Technical Changes

*   **Database:** Added `priority` (INTEGER) column to `file_health` table.
*   **API:** Added endpoints for `POST /health/bulk/repair` and updated Metrics API to expose provider error maps.
*   **Docker:** Updated Dockerfile and build process to support custom image tags (`alpha6`).