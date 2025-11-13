# Health Monitoring Configuration

AltMount provides comprehensive health monitoring capabilities that detect corrupted files and can automatically coordinate repairs through your ARR applications. This guide covers configuring health monitoring for optimal media collection integrity.

## Overview

AltMount's health monitoring system continuously watches for file corruption and integrity issues across your media collection. When issues are detected, AltMount can automatically notify your ARR applications to re-download the affected content.

## Basic Health Monitoring Configuration

### Core Health Settings

Configure health monitoring through the System Configuration interface:

![Health Monitoring](../../static/img/health_config.png)

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
- **Recovery**: Metadata can be recreated during re-import if files are re-downloaded
- **Use Case**: Keeps your system clean when files are intentionally removed from either side

**⚠️ Important Considerations:**

- Enable cleanup only if you're certain you want automatic removal of orphaned files
- Library Directory must be properly configured for cleanup to work
- Files will be permanently deleted during cleanup operations
- Consider disabling during migration or testing phases

**Metadata-Only Sync (Import Strategy: NONE)**

When your import strategy is set to `NONE`, the system performs a simplified metadata-only sync:

- **No Library Scanning**: Skips library directory scanning entirely
- **Metadata-Based Discovery**: Only syncs database with metadata files
- **Direct Access**: Files accessed directly via WebDAV mount without library intermediary
- **Library Path**: Health records have `library_path` set to `null`
- **No Cleanup Operations**: Bidirectional cleanup is not performed (no library to sync with)
- **Use Case**: Ideal when using WebDAV mount directly without symlinks or STRM files

**Sync Behavior Comparison:**

| Feature | Full Sync (Symlinks/STRM) | Metadata-Only (NONE) |
|---------|---------------------------|----------------------|
| Library Directory Scan | ✅ Yes | ❌ No |
| Metadata Scan | ✅ Yes | ✅ Yes |
| Import Directory Scan | ✅ Yes | ❌ No |
| Bidirectional Cleanup | ✅ Optional | ❌ N/A |
| Library Path Tracking | ✅ Yes | ❌ Null |
| Performance | Moderate | Fast |

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

- Newer files are more likely to have issues (encoding problems, incomplete uploads)
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

- **Full Validation**: Set `segment_sample_percentage` to 100
  - Checks all file segments
  - More thorough but slower
  - Recommended for critical files or when issues are suspected
  - Example: 10GB file with 1000 segments → checks all 1000 segments

**Validation Process:**

1. System calculates number of segments to check based on `segment_sample_percentage`
2. Randomly selects segments across the file for statistical reliability
3. Attempts to download selected segments from Usenet using configured connections
4. Validates segment availability and integrity
5. Records results in health database
6. Triggers repair if file fails validation after retry attempts

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

**Retry Logic:**

The system implements two-phase retry logic with hardcoded limits:

1. **Health Check Retries**: 2 attempts

   - Number of validation attempts before triggering repair
   - Accounts for temporary network issues or transient failures
   - After exhaustion, file transitions to repair phase

2. **Repair Retries**: 3 attempts
   - Number of repair coordination attempts before marking as permanently failed
   - Prevents infinite repair loops for consistently failing files
   - After exhaustion, file marked as permanently corrupted

**Repair Coordination Requirements:**

For automatic repair to function:

- ✅ ARR integration must be enabled and configured
- ✅ Valid API keys for ARR instances (Sonarr/Radarr)
- ✅ Proper mount path alignment between AltMount and ARRs
- ✅ ARR applications must have access to library directory
- ✅ Library directory properly configured in health settings

**When Repair Fails:**

If repair attempts are exhausted:

- File marked as permanently corrupted in database
- Status visible via API and web interface
- Manual intervention required
- Consider checking:
  - Usenet provider availability
  - NZB file quality and completeness
  - ARR search indexers configuration

## Health Monitoring Behavior

### Default Configuration (Logging Only)

By default, AltMount health monitoring only logs corrupted files without taking action:

**Default Behavior:**

- ✅ **Corruption Detection**: Identifies and logs corrupted files
- ✅ **Status Tracking**: Maintains health status in database
- ✅ **API Reporting**: Provides health information via REST API
- ❌ **Automatic Repair**: Does not trigger re-downloads (disabled by default)

**Log Example:**

```log
WARN Health monitor detected corrupted file: /metadata/movies/Movie.2023.1080p/Movie.2023.1080p.mkv
INFO Health status updated for: Movie.2023.1080p
```

This conservative approach prevents unwanted re-downloads while still providing visibility into collection health.

### Automatic Repair Requirements

Automatic repair is enabled by default when ARR integration is properly configured. There is no separate toggle - the system automatically coordinates repairs when:

**Required Configuration:**

1. **ARR Integration Enabled**: ARR integration must be configured (see ARR Integration documentation)
2. **ARR Instances Configured**: At least one Radarr or Sonarr instance properly configured
3. **Library Directory Set**: Health monitoring `library_dir` must be configured
4. **API Access**: Valid API keys for ARR instances
5. **Mount Path Alignment**: ARR and AltMount must have consistent path mapping

**How It Works:**

- When a file fails health validation after retries, the system automatically triggers repair
- No manual intervention or configuration toggle required
- Repair coordination happens transparently through the ARR API
- Files that cannot be repaired after 3 attempts are marked as permanently corrupted

### Health Status Dashboard

Monitor the health of your collection through the web interface:

![Health Status Dashboard](../../static/img/health_monitoring.png)
_Health status overview showing collection integrity metrics_

**Health Dashboard Features:**

- **Overall Health Score**: Percentage of healthy files in collection
- **Recent Issues**: List of recently detected corrupted files
- **Repair Activity**: Status of automatic repair operations
- **Historical Trends**: Health metrics over time

## ARR Integration Requirements

### Enabling ARR Integration for Health Monitoring

![Arr configuration](../../static/img/arr_config.png)

Auto-repair requires properly configured ARR integration:

```yaml
# Root-level mount path configuration
mount_path: "/mnt/altmount" # Must match ARR WebDAV mount path

arrs:
  enabled: true # Required for auto-repair
  max_workers: 5 # Concurrent repair operations
  radarr_instances:
    - name: "radarr-main"
      url: "http://localhost:7878"
      api_key: "your-radarr-api-key"
      enabled: true
      sync_interval_hours: 24 # Optional periodic sync
  sonarr_instances:
    - name: "sonarr-main"
      url: "http://localhost:8989"
      api_key: "your-sonarr-api-key"
      enabled: true
      sync_interval_hours: null # Null disables periodic sync
```

### Critical ARR Configuration

**Mount Path Alignment:**

The root-level `mount_path` must exactly match where your ARRs access the WebDAV mount:

- **Root mount_path**: `/mnt/altmount` (where ARRs see WebDAV files)
- **ARR Library Paths**: Must be under `/mnt/altmount/`
- **Consistency**: All ARR instances must use the same mount path
- **Configuration Location**: `mount_path` is at root config level, not inside arrs section

**Example ARR Configuration:**

```yaml
# Radarr library configuration
Movies Library Path: /mnt/altmount/movies/

# Sonarr library configuration
TV Shows Library Path: /mnt/altmount/tv/
```

## Health Monitoring Features

### Corruption Detection Methods

AltMount detects file corruption through multiple methods:

**Active Detection:**

- **Missing Articles**: Articles missing from the file
- **Playback Issues**: Detection of unplayable or damaged files

### Repair Process

When auto-repair is enabled and corruption is detected:

**Repair Workflow:**

1. **Corruption Detection**: Health monitor identifies corrupted file
2. **Content Identification**: Matches file to movie/TV show in ARR
3. **File Removal**: Removes the file from the library
4. **ARR Notification**: Sends re-download request to appropriate ARR
5. **Re-download Monitoring**: Tracks ARR re-download progress
6. **File Import**: ARR Imports the file into the library
7. **Altmount Import**: AltMount Imports the file into the metadata and replaces the old file

## Next Steps

With health monitoring configured:

1. **[Troubleshooting](../5.%20Troubleshooting/common-issues.md)** - Resolve health monitoring issues

---

Health monitoring ensures your media collection remains intact and automatically repairs issues when they're detected. Start with logging-only mode and gradually enable auto-repair as you become comfortable with the system.
