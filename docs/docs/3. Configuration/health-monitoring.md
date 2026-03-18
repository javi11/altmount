---
title: Health Monitoring
description: Detect corrupted files and automatically coordinate repairs via Sonarr/Radarr.
keywords: [altmount, health monitoring, corrupted files, repair, arr, sonarr, radarr, usenet]
---

# Health Monitoring

AltMount monitors your media files for corruption and can automatically trigger re-downloads through Sonarr/Radarr when issues are found.

## Quick Start

1. Go to **Configuration → Health Monitoring**
2. Enable the **Master Engine**
3. Set your **Library Parent Directory** (where symlinks/STRM files live)
4. Optionally enable the **Repair Engine** for automatic ARR re-downloads

That's it — AltMount will start discovering and validating files automatically.

---

## Configuration

![Health Configuration](/images/config-health.png)

Navigate to **Configuration → Health Monitoring**.

### Master Engine

Top toggle — enables file discovery and health validation. The three-step workflow:
1. Discover files via periodic library sync
2. Validate Usenet integrity using sampling or deep checks
3. Unhealthy files are automatically replaced in Sonarr/Radarr (when repair is enabled)

### Repair Engine

Separate toggle for automatic ARR re-downloads.

| Setting | Default | Description |
|---------|---------|-------------|
| Base Interval | 60 min | Wait before first re-notification |
| Max Cooldown | 24 hours | Maximum delay between attempts |
| Max Repair Retries | 3 | Attempts before marking permanently corrupted |
| Exponential Back-off | On | Doubles interval each attempt (1h → 2h → 4h...) |
| Resolve on Import | Off | Auto-clear repair status when file is re-imported |

### Library Directory

Set the **Library Parent Directory** — where your symlinks or STRM files live. This must match where your ARR applications look for media files.

### Advanced Settings

Expand **Performance & Deep Validation** for:

**Validation:**

| Setting | Default | Description |
|---------|---------|-------------|
| Verify Every Segment | Off | Check 100% of segments instead of sampling |
| Ghost File Detection | Off | Verify actual content (uses bandwidth) |
| Sampling Percentage | 5% | Segments to check when sampling (1-100%) |
| Acceptable Missing | 0% | Missing segment tolerance (0-10%) |

**Scheduling & Concurrency:**

| Setting | Default | Description |
|---------|---------|-------------|
| Parallel Processing | 1 | Concurrent health check jobs |
| Sync Interval | 360 min | Library sync frequency (0 = disabled) |
| Health Check Loop | 5s | Worker polling interval |
| Sync Concurrency | 5 | Parallel workers during sync |
| Health Connections | 5 | NNTP connections for checks |

---

## Library Sync & Metadata Cleanup

Library sync keeps the health database, metadata files, and library directory in agreement. It runs periodically (default: every 6 hours) and can also be triggered manually from the Health page sidebar card.

### How Sync Works

During each sync, AltMount:

1. Scans the library directory for symlinks/STRM files
2. Scans the metadata directory for `.meta` and `.id` sidecar files
3. Matches library entries to metadata entries
4. Creates health check records for newly discovered files
5. Identifies orphaned entries on either side

### Sync Modes

**Full Sync** — When using symlinks or STRM files (default import strategy):
- Scans both library directory and metadata directory
- Tracks `library_path` for each health record
- Supports bidirectional orphan cleanup

**Metadata-Only Sync** — When import strategy is set to `NONE`:
- Skips library directory scanning entirely
- Only syncs database with metadata files
- Health records have no `library_path`
- No cleanup operations (no library to compare against)
- Ideal for direct WebDAV access without symlinks

### Orphan Cleanup

When **Orphan Cleanup** is enabled in the configuration, sync performs bidirectional cleanup:

- **Metadata without library file** → metadata deleted
- **Library file without metadata** → library file deleted

This keeps both sides consistent when files are intentionally removed from either location.

:::warning
Orphan cleanup permanently deletes files. Use the **Dry Run Test** button on the configuration page first — it shows exactly what would be deleted (orphaned metadata count, orphaned library files, and database records to clean) without making any changes.
:::

**When to enable cleanup:**
- Your library is stable and you want automatic consistency
- You've verified the dry run results look correct

**When to keep it disabled:**
- During migration or initial setup
- If you're testing import strategies
- If you manually manage metadata files

### Sync Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| Sync Interval | 360 min | How often sync runs automatically (0 = disabled) |
| Sync Concurrency | 5 | Parallel workers during sync (0 = auto) |
| Orphan Cleanup | Off | Enable bidirectional cleanup |

---

## How Health Checks Work

### Smart Scheduling

Files are checked based on age using exponential backoff:

| File Age | Next Check In |
|----------|---------------|
| 1 day | ~2 days |
| 1 week | ~2 weeks |
| 1 month | ~2 months |
| 1 year | ~2 years |

Newer files get checked more often since they're more likely to have issues. Minimum interval: 1 hour.

### Validation

By default, 5% of segments are randomly sampled — statistically sufficient since corrupted files tend to have many missing segments.

**Choosing sample percentage:**
- **5% (default)**: Good for large libraries
- **10-20%**: Higher confidence, moderate cost
- **100%**: After provider changes or suspected issues (use temporarily, then lower back)

### Automatic Repair

1. File fails validation after 2 retries → marked for repair
2. AltMount sends rescan to Sonarr/Radarr (AltMount never deletes files)
3. ARR searches indexers and re-downloads
4. New file enters health check queue

Repair uses exponential backoff (up to 3 attempts). Files that can't be repaired are marked permanently corrupted — usually meaning content is no longer on Usenet.

---

## ARR Integration & Webhooks

### Path Configuration

AltMount uses three paths that must be configured correctly for health monitoring, repair, and webhook cleanup to work:

| Path | Config Key | Purpose | Example |
|------|-----------|---------|---------|
| **Mount Path** | `mount_path` (root level) | Where ARRs access the WebDAV mount | `/mnt/remotes/altmount` |
| **Library Directory** | `health.library_dir` | Where symlinks/STRM files are created | `/mnt/library` |
| **Import Directory** | `import.dir` | Where import strategy places links | `/mnt/imports` |

When AltMount receives a webhook from Sonarr/Radarr, it normalizes the incoming path by stripping the longest matching prefix from these three paths. This converts absolute ARR paths into relative paths that AltMount uses internally.

**Example**: ARR sends `/mnt/remotes/altmount/complete/movies/Movie.2024.mkv` → AltMount normalizes to `complete/movies/Movie.2024.mkv`.

:::warning Path Alignment
The `mount_path` must exactly match where your ARR applications access the WebDAV mount. If these don't match, webhooks won't be able to find the correct health records or metadata to clean up.
:::

**Correct configuration example:**

```yaml
# Root level — where ARRs mount AltMount via WebDAV/rclone
mount_path: "/mnt/remotes/altmount"

# Import — where symlinks/STRM files are placed after import processing
import:
  dir: "/mnt/imports"
  strategy: "SYMLINK"  # or "STRM" or "NONE"

# Health — where your final library lives (e.g. where Plex/Jellyfin reads from)
health:
  enabled: true
  library_dir: "/mnt/library"

# ARR instances — their root folders must be under mount_path
arrs:
  enabled: true
  radarr_instances:
    - name: "radarr-main"
      url: "http://localhost:7878"
      api_key: "your-api-key"
      enabled: true
  sonarr_instances:
    - name: "sonarr-main"
      url: "http://localhost:8989"
      api_key: "your-api-key"
      enabled: true
```

In Sonarr/Radarr, your root folders should point under `mount_path`:
```
Radarr Root Folder:  /mnt/remotes/altmount/movies/
Sonarr Root Folder:  /mnt/remotes/altmount/tv/
```

### ARR Webhook Events

AltMount automatically registers a webhook with each configured ARR instance. When Sonarr/Radarr performs actions, it sends events that AltMount handles:

| Event | Trigger | What AltMount Does |
|-------|---------|-------------------|
| **Download** | File imported | Adds health record with high priority |
| **Upgrade** | File replaced by better quality | Adds new file to health, deletes old file's metadata + health record |
| **Rename** | File path changed | Updates health record path |
| **MovieFileDelete** | Single movie file deleted | Deletes health record + metadata |
| **EpisodeFileDelete** | Single episode file deleted | Deletes health record + metadata |
| **MovieDelete** | Entire movie removed | Deletes all health records + metadata in that directory |
| **SeriesDelete** | Entire series removed | Deletes all health records + metadata in that directory |

### How Webhook Deletion Works

When Sonarr/Radarr deletes a file or series/movie, AltMount automatically cleans up:

1. **Receives the webhook** with the absolute file/folder path from ARR
2. **Looks up the health record** — first by `library_path` (the absolute ARR path), falling back to the normalized `file_path`
3. **Deletes the health record** from the database
4. **Deletes the metadata** (`.meta` and `.id` sidecar files) and optionally the source NZB

For directory deletions (MovieDelete, SeriesDelete), AltMount deletes all health records and metadata within that directory prefix.

:::tip
This means when you delete a movie in Radarr or a series in Sonarr, AltMount automatically cleans up all associated metadata and health tracking — no manual cleanup needed.
:::

### STRM File Handling

When using the STRM import strategy, ARR may send webhook paths ending in `.strm`. AltMount reads the `.strm` file content, extracts the actual file path from the URL query parameter, and uses that for health record matching. This is transparent — no special configuration needed.

### Without ARR Integration

Health monitoring runs in **logging-only mode** — it detects and reports corruption but cannot trigger repairs or receive webhook events for automatic cleanup.

---

## YAML Reference

```yaml
health:
  enabled: true
  library_dir: "/path/to/library"
  cleanup_orphaned_metadata: false
  check_interval_seconds: 5
  max_connections_for_health_checks: 5
  max_concurrent_jobs: 1
  segment_sample_percentage: 5
  library_sync_interval_minutes: 360
  library_sync_concurrency: 5
  resolve_repair_on_import: false
  verify_data: false
  check_all_segments: false
  acceptable_missing_segments_percentage: 0.0
  read_timeout_seconds: 10

  repair:
    enabled: true
    interval_minutes: 60
    max_cooldown_hours: 24
    exponential_backoff: true
    max_repair_retries: 3
```

---

## API Reference

| Operation | Method | Endpoint |
|-----------|--------|----------|
| List health records | GET | `/api/health` |
| Health statistics | GET | `/api/health/stats` |
| Corrupted files | GET | `/api/health/corrupted` |
| Trigger repair | POST | `/api/health/{id}/repair` |
| Immediate check | POST | `/api/health/{id}/check-now` |
| Bulk repair | POST | `/api/health/bulk/repair` |
| Delete record | DELETE | `/api/health/{id}` |
| Start library sync | POST | `/api/health/library-sync/start` |
| Cancel sync | POST | `/api/health/library-sync/cancel` |
| Dry run sync | POST | `/api/health/library-sync/dry-run` |
| Sync status | GET | `/api/health/library-sync/status` |
| Reset all checks | POST | `/api/health/reset-all` |
| Regenerate symlinks | POST | `/api/health/regenerate-symlinks` |

---

## Next Steps

- **[Configure ARR Integration](integration.md)** — Required for automatic repairs
- **[Troubleshooting](../5.%20Troubleshooting/common-issues.md)** — Common health monitoring issues
