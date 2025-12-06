# Changelog

## v0.6.0-alpha6 - 2025-12-05

This update represents a major overhaul of the `altmount` Health and Import systems, focusing on **concurrency**, **observability**, and **automation**. It transforms the Health system from a passive scanner into an active, parallelized repair engine with deep insights into provider performance.

### ✨ New Features

*   **Provider Health Dashboard:**
    *   New dedicated tab in the Health UI.
    *   **Real-time Metrics:** View download traffic, total articles, and global error counts.
    *   **Connection Monitoring:** See "Active Connections" vs "Max Connections" (e.g., `45 / 100`).
    *   **Error Attribution:** Identifies which specific Usenet provider is causing the *most* errors relative to others (error percentage relative to total errors).
    *   **Privacy:** Provider usernames are blurred by default and revealed on hover.

*   **Instant Import ("Push" Notifications):**
    *   `altmount` now proactively notifies Sonarr/Radarr immediately after a download completes.
    *   Bypasses the standard 1-minute polling interval, resulting in near-instant imports.
    *   Supports both `DownloadedEpisodesScan` (TV) and `DownloadedMoviesScan` (Movies).

*   **Bulk Repair Actions:**
    *   Added ability to select multiple "Corrupted" files in the UI.
    *   New **"Repair Selected"** toolbar action triggers redownloads for all selected items in one go.

*   **Concurrent Health Checks:**
    *   Health checking is now parallelized.
    *   Configurable `max_concurrent_jobs` (default: 1, recommended: 5-15) allows checking multiple files simultaneously, significantly clearing the "Pending" queue faster.

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