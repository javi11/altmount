# Health Monitoring Configuration

AltMount provides comprehensive health monitoring capabilities that detect corrupted files and can automatically coordinate repairs through your ARR applications. This guide covers configuring health monitoring for optimal media collection integrity.

## Overview

AltMount's health monitoring system continuously watches for file corruption and integrity issues across your media collection. When issues are detected, AltMount can automatically notify your ARR applications to re-download the affected content.

## Basic Health Monitoring Configuration

### Core Health Settings

Configure health monitoring through the System Configuration interface:

```yaml
health:
  enabled: true # Enable health monitoring service
  library_dir: "/path/to/library" # Path to library directory (required)
  cleanup_orphaned_metadata: false # Enable bidirectional cleanup during sync
  check_interval_seconds: 5 # Worker check interval (default: 5)
  max_connections_for_health_checks: 5 # NNTP connections per check
  segment_sample_percentage: 5 # Percentage of segments to validate (5-100)
  library_sync_interval_minutes: 60 # Library sync frequency (0 = disabled)
  library_sync_concurrency: 10 # Parallel workers during sync
```

**Configuration Options:**

- **enabled**: Master toggle for health monitoring service
- **library_dir**: Path to your library directory where symlinks/STRM files reside (required for sync)
- **cleanup_orphaned_metadata**: When enabled, performs bidirectional cleanup of orphaned metadata and library files
- **check_interval_seconds**: How often the worker checks for files needing validation (default: 5 seconds)
- **max_connections_for_health_checks**: NNTP connections used per segment during health checks (default: 5)
- **segment_sample_percentage**: Percentage of file segments to check (default: 5%, use 100 for full validation)
- **library_sync_interval_minutes**: How often to sync with library directory (default: 60 minutes, 0 to disable)
- **library_sync_concurrency**: Number of parallel workers during library sync operations (default: 10)

**Health Monitoring Components:**

- **Corruption Detection**: Monitors file access and playback for corruption indicators
- **Integrity Validation**: Checks file completeness and consistency based on configured settings
- **Repair Coordination**: Interfaces with ARR applications for automatic re-downloads when auto-repair is enabled
- **Status Reporting**: Provides health status through API and web interface

## How the Health System Works

The health monitoring system operates through an intelligent multi-stage workflow that automatically discovers, validates, and repairs files in your media library.

### Discovery & Synchronization

**Periodic Library Sync**

The system periodically syncs with your library directory to discover and track files:

- **Sync Frequency**: Configurable interval (default: every 60 minutes)
- **Manual Triggers**: Can be triggered manually via API or disabled by setting interval to 0
- **Discovery Process**: During each sync, the system:
  - Discovers new files added to the library
  - Updates file metadata and tracking information
  - Identifies files removed from the library
  - Creates health check records with smart scheduling

**Full Sync Cleanup Behavior**

When "Cleanup Orphaned Metadata Files" is enabled, the system performs bidirectional cleanup:

- **Metadata Cleanup**: Metadata files without corresponding library files are permanently deleted
- **Library Cleanup**: Files in the library pointing to missing metadata are also deleted
- **Bidirectional Process**: This ensures consistency between metadata and library:
  - If library file exists but metadata is missing → library file deleted
  - If metadata exists but library file is missing → metadata deleted
- **Use Case**: Keeps your system clean when files are intentionally removed from either side

**⚠️ Important Considerations:**

- Enable cleanup only if you're certain you want automatic removal of orphaned files
- Library Directory must be properly configured for cleanup to work
- Files will be permanently deleted during cleanup operations

**Metadata-Only Sync (Import Strategy: NONE)**

When your import strategy is set to `NONE`, the system performs a simplified metadata-only sync:

- **Metadata-Based Discovery**: Only syncs database with metadata files
- **Direct Access**: Files accessed directly via WebDAV mount without library intermediary
- **No Cleanup Operations**: Bidirectional cleanup is not performed since is not needed

**Sync Behavior Comparison:**

| Feature                | Full Sync (Symlinks/STRM) | Metadata-Only (NONE) |
| ---------------------- | ------------------------- | -------------------- |
| Library Directory Scan | ✅ Yes                    | ❌ No                |
| Metadata Scan          | ✅ Yes                    | ✅ Yes               |
| Import Directory Scan  | ✅ Yes                    | ❌ No                |
| Bidirectional Cleanup  | ✅ Optional               | ❌ N/A               |
| Performance            | Moderate                  | Fast                 |

### Health Check Scheduling

**Smart Scheduling Algorithm**

Files are checked using an intelligent exponential backoff algorithm based on their release date:

**Formula**: `NextCheck = ReleaseDate + 2 × (NOW - ReleaseDate)`

**Examples:**

- **1 day old file**: Next check in 2 days (released 1 day ago → check after 2 days total)
- **1 week old file**: Next check in 2 weeks (released 1 week ago → check after 2 weeks total)
- **1 month old file**: Next check in 2 months
- **1 year old file**: Next check in 2 years

**Minimum Interval**: 1 hour (prevents excessive checking of very new files)

**Rationale:**

- Newer files are more likely to have issues (DCMA takedown, incomplete uploads)
- Older, stable files have proven reliability and need less frequent validation
- Exponential backoff optimizes system resources while maintaining reliability

### Health Validation

**File Integrity Checks**

Files are validated through configurable integrity checks:

**Validation Methods:**

The system uses percentage-based segment sampling for validation:

- **Sampling Mode** (default): Set `segment_sample_percentage` to a value between 1-99%

  - Default: 5% of segments
  - Faster validation, good for large files
  - Statistically reliable for corruption detection
  - Example: 10GB file with 1000 segments → checks 50 segments

**Validation Process:**

1. System calculates number of segments to check based on `segment_sample_percentage`
2. Randomly selects segments across the file for statistical reliability
3. Validates segment availability and integrity
4. Records results in health database
5. Triggers repair if file fails validation after retry attempts

**Configuration Options:**

- `max_connections_for_health_checks`: NNTP connections per check (default: 5)
- `segment_sample_percentage`: Percentage of segments to validate (default: 5%, range: 1-100)

### Automatic Repair

**Repair Workflow**

When unhealthy files are detected, the system automatically coordinates repairs with your ARR applications:

**Step 1: Detection**

- File fails health validation after retry attempts exhausted
- System identifies the file as corrupted in health database
- Health record transitions to repair phase

**Step 2: ARR Rescan Trigger**

- System retrieves library path from health record
- Sends rescan request to associated ARR application for that specific file
- ARR detects the file needs redownload and schedules it
- **Note**: AltMount doesn't delete files directly - ARR handles file management

**Step 3: ARR Redownload Process**

- ARR searches for the content through its indexers
- Downloads replacement file through AltMount
- ARR deletes old corrupted file and imports new one
- New file stored in library with fresh metadata

**Step 4: Validation**

- Newly downloaded file enters health check queue
- Scheduled based on smart scheduling algorithm
- Monitored for integrity with fresh health record

**When Repair Fails:**

If repair attempts are exhausted:

- File marked as permanently corrupted in database
- Status visible via API and web interface
- Manual intervention required

## Health Monitoring Behavior

### Default Configuration (Logging Only)

By default, AltMount health monitoring only logs corrupted files without taking action:

**Default Behavior:**

- ✅ **Corruption Detection**: Identifies and logs corrupted files
- ✅ **Status Tracking**: Maintains health status in database
- ✅ **API Reporting**: Provides health information via REST API
- ❌ **Automatic Repair**: Does not trigger re-downloads (disabled by default)

## Next Steps

With health monitoring configured:

1. **[Troubleshooting](../5.%20Troubleshooting/common-issues.md)** - Resolve health monitoring issues

---
