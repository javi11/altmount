---
sidebar_position: 5
title: Postie Fallback Integration
---

# Postie Fallback Integration (Optional Feature)

## Overview

The Postie integration provides an **automated fallback workflow** for when AltMount cannot stream content from the configured Usenet providers. When enabled, failed downloads are automatically uploaded to alternative Usenet backbones via Postie, then re-imported with the new message IDs.

> **⚠️ Important**: This is an **optional feature** that requires SABnzbd fallback to be configured first. If Postie is disabled, SABnzbd fallback works independently as it does now.

## How It Works

```
┌─────────────────────────────────────────────────────────────────────────┐
│                     SONARR/RADARR TRIGGERS DOWNLOAD                     │
│  Sends NZB to AltMount (as SABnzbd download client)                    │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                     ALTMOUNT TRIES STREAMING                            │
│  Attempts to stream from configured NNTP providers (backbone A)         │
│                            ↓ FAILS                                       │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                     SABNZBD FALLBACK TRIGGERED                           │
│  - Sends NZB to SABnzbd                                                 │
│  - Stores original_release_name in queue metadata                       │
│  - Marks PostieUploadStatus as "pending"                                │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                     SABNZBD DOWNLOADS CONTENT                           │
│  Files saved to: /downloads/completed                                   │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                     POSTIE WATCHER DETECTS FILES                        │
│  - Watches /downloads/completed                                         │
│  - Checks file stability                                                │
│  - Uses PARTIAL obfuscation (filenames preserved!)                      │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                     POSTIE UPLOADS TO NEW BACKBONES                     │
│  - Uploads to configured servers (backbone B)                           │
│  - Creates NEW NZB with NEW message IDs                                 │
│  - Filename is PRESERVED (partial obfuscation)                          │
│  - Deletes original video files (delete_original_file: true)            │
│  - Saves NZB to: /watch (AltMount's watch directory)                    │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                     ALTMOUNT WATCHER DETECTS NZB                        │
│  - Scans /watch every 10 seconds                                         │
│  - Calls PostieMatcher to find original queue item                      │
│  - Matches by: original_release_name, timestamp, category               │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                     ALTMOUNT RE-IMPORTS WITH TRACKING                    │
│  - Imports NZB with new message IDs                                     │
│  - Updates ORIGINAL queue item (not creates new one)                    │
│  - Updates PostieUploadStatus to "completed"                            │
│  - Creates symlink/STRM based on user's import strategy                 │
│  - Triggers existing ARR notification (arr_notifier.go)                │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                     SONARR/RADARR NOTIFIED & IMPORTS                     │
│  - ARR receives "RefreshMonitoredDownloads" command                    │
│  - ARR scans import directory, finds symlink/STRM file                 │
│  - ARR copies file to its own organized location                       │
│  - ARR renames and organizes: /tv/Show Name/Season 1/Episode.mkv       │
│  - ARR updates database, triggers media server scan                     │
│  - SUCCESS! Content available from new backbones                        │
└─────────────────────────────────────────────────────────────────────────┘
```

## Prerequisites

⚠️ **Required**: SABnzbd fallback must be configured first

Postie integration **builds on top of** SABnzbd fallback. Without SABnzbd fallback configured, Postie cannot function.

### Step 1: Configure SABnzbd Fallback (Required)

In AltMount's web interface or configuration file:

```yaml
sabnzbd:
  enabled: true
  fallback_host: "http://sabnzbd:8080"  # Your SABnzbd URL
  fallback_api_key: "your-api-key"     # Your SABnzbd API key
```

## Setup Guide

### Understanding the Watch Directories

There are **three key directories** in the Postie integration. Understanding how they work together is important:

| Directory | Purpose | Who Writes | Who Reads | Required? |
|-----------|---------|------------|------------|-----------|
| `/downloads/completed` | SABnzbd's download completion folder | SABnzbd | Postie | Yes - SABnzbd output |
| `/watch` | Where Postie writes NZBs for AltMount | Postie | AltMount watcher | Yes - Postie output |
| `/import` | AltMount's main watch directory for manual NZBs | User (manual) | AltMount watcher | No - optional |

**Important**: You do **not** need to configure multiple watch directories. The system uses:

1. **SABnzbd** downloads to `/downloads/completed` (its internal directory)
2. **Postie** watches `/downloads/completed` and outputs NZBs to `/watch`
3. **AltMount** watches `/watch` for Postie-generated NZBs

The `/import` directory is separate and only used for manual NZB drops by users.

### Step 2: Configure Postie

1. **Install Postie** (separate service):
   ```bash
   docker pull ghcr.io/javi11/postie:latest
   ```

2. **Create Postie configuration** with **partial obfuscation**:

   ```yaml
   # config.yaml
   watcher:
     enabled: true
     watch_directory: "/downloads/completed"  # SABnzbd's completed folder
     delete_original_file: true  # Delete video files after upload!

   posting:
     obfuscation_policy: partial  # CRITICAL: Preserve filenames for matching
     par2_obfuscation_policy: none

     # Configure your upload servers (alternative backbones)
     servers:
       - name: "backup-provider"
         host: "news.example.com"
         port: 563
         username: "user"
         password: "pass"
         ssl: true
         connections: 10

   nzb:
     output_dir: "/watch"  # Where to write NZBs for AltMount
   ```

**Why partial obfuscation?**
- **Subject**: Obfuscated (privacy)
- **Filename**: Preserved (matching)
- This allows AltMount to identify the content while still providing privacy

### Step 3: Complete Docker Compose Setup

Here's a complete Docker Compose example with all three services and proper volume sharing:

```yaml
services:
  # ========================================
  # AltMount - Main Application
  # ========================================
  altmount:
    image: ghcr.io/javi11/altmount:latest
    container_name: altmount
    ports:
      - "8080:8080"  # Web UI and API
    volumes:
      - altmount_data:/data                              # Application data
      - ./watch:/watch                                  # Postie writes NZBs here
    environment:
      # SABnzbd API Configuration
      - SABNZBD_ENABLED=true
      - SABNZBD_COMPLETE_DIR=/                           # Root of mount

      # SABnzbd Fallback (required for Postie)
      - SABNZBD_FALLBACK_HOST=http://sabnzbd:8080
      - SABNZBD_FALLBACK_API_KEY=${SABNZBD_API_KEY}

      # Postie Integration (optional)
      - POSTIE_ENABLED=true
      - POSTIE_WATCH_DIR=/watch
      - POSTIE_TIMEOUT_MINUTES=120

      # Import Strategy
      - IMPORT_STRATEGY=SYMLINK                          # or STRM or NONE
      - IMPORT_DIR=/import                               # For manual NZBs

  # ========================================
  # SABnzbd - Fallback Download Client
  # ========================================
  sabnzbd:
    image: linuxserver/sabnzbd:latest
    container_name: sabnzbd
    ports:
      - "8081:8080"  # SABnzbd web UI
    volumes:
      - ./downloads/config:/config                       # SABnzbd config
      - ./downloads/incomplete:/downloads/incomplete     # Incomplete downloads
      - ./downloads/completed:/downloads/completed       # Completed downloads - Postie watches this
    environment:
      - TZ=${TZ:-Etc/UTC}
      - SABNZBD_WHITELIST=*                             # Allow external API access
      - SABNZBD_API_KEY=${SABNZBD_API_KEY}              # API key for AltMount

  # ========================================
  # Postie - Usenet Uploader for Fallback
  # ========================================
  postie:
    image: ghcr.io/javi11/postie:latest
    container_name: postie
    volumes:
      # Watch SABnzbd's completed folder for new downloads
      - ./downloads/completed:/downloads/completed:ro    # :ro = read-only (Postie doesn't write here)

      # Write NZBs for AltMount to pick up
      - ./watch:/watch                                   # AltMount watches this

      # Postie configuration
      - ./postie_config:/config
    command: ["watch", "-config", "/config/config.yaml"]
    environment:
      - TZ=${TZ:-Etc/UTC}
    # Postie will restart automatically if it crashes
    restart: unless-stopped

  # ========================================
  # Sonarr - TV Series Management
  # ========================================
  sonarr:
    image: linuxserver/sonarr:latest
    container_name: sonarr
    ports:
      - "8989:8989"
    volumes:
      - ./sonarr/config:/config
      - ./tv:/tv                                       # Media library
      # Mount AltMount's symlink directory for imports
      - ./symlinks:/symlinks                            # AltMount creates symlinks here
    environment:
      - TZ=${TZ:-Etc/UTC}

  # ========================================
  # Radarr - Movie Management
  # ========================================
  radarr:
    image: linuxserver/radarr:latest
    container_name: radarr
    ports:
      - "7878:7878"
    volumes:
      - ./radarr/config:/config
      - ./movies:/movies                                # Media library
      # Mount AltMount's symlink directory for imports
      - ./symlinks:/symlinks                            # AltMount creates symlinks here
    environment:
      - TZ=${TZ:-Etc/UTC}

# ========================================
# Named Volumes
# ========================================
volumes:
  altmount_data:
    driver: local
```

**Directory Structure Explained:**

```
your-project/
├── docker-compose.yml
├── .env                    # Contains API keys
├── watch/                  # Postie → AltMount (Postie writes, AltMount reads)
├── downloads/
│   ├── completed/          # SABnzbd → Postie (SABnzbd writes, Postie reads)
│   └── incomplete/         # SABnzbd internal
├── symlinks/               # AltMount → Sonarr/Radarr (AltMount writes, ARRs read)
├── postie_config/          # Postie configuration
├── sonarr_config/
└── radarr_config/
```

### Step 4: Enable Postie in AltMount UI

1. Navigate to **Configuration** → **SABnzbd API Configuration**
2. Ensure SABnzbd fallback is configured (Host and API Key)
3. Scroll to **Postie Integration** section
4. Enable Postie integration
5. Configure:
   - **Watch Directory**: Leave empty to use `/watch` (recommended)
   - **Timeout Minutes**: How long to wait for Postie upload (default: 120)

### Step 5: Configure Sonarr/Radarr Download Clients

1. In Sonarr/Radarr, go to **Settings** → **Download Clients**
2. Add a new **SABnzbd** client:
   - **Host**: `altmount`
   - **Port**: `8080`
   - **API Key**: Your Sonarr/Radarr API key (AltMount uses this for callbacks)
   - **Category**: `tv` (Sonarr) or `movies` (Radarr)

## How Sonarr/Radarr Import Works

AltMount already handles the complete Sonarr/Radarr import flow. When Postie uploads are re-imported, they follow the **exact same flow**:

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    ALTMOUNT COMPLETES NZB IMPORT                        │
│  - Content is streamable from Usenet (or new Postie backbones)       │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    ALTMOUNT CREATES SYMLINK/STRM                        │
│  Based on user's import strategy setting:                              │
│  - NONE: No symlink (direct WebDAV access)                             │
│  - SYMLINKS: Creates symlink to import directory                       │
│  - STRM: Creates .strm file with streaming URL                         │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    ALTMOUNT TRIGGERS ARR NOTIFICATION                  │
│  - Calls existing arr_notifier.go                                      │
│  - Sends "RefreshMonitoredDownloads" to Sonarr/Radarr                  │
│  - Sonarr/Radarr scans their import directories                        │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    SONARR/RADARR IMPORTS THE FILE                      │
│  - Finds the symlink/STRM file                                         │
│  - Copies/renames/organizes according to its config                    │
│  - Updates its database                                                │
│  - Triggers media server (Plex/Jellyfin) scan                          │
└─────────────────────────────────────────────────────────────────────────┘
```

## Configuration Reference

### Environment Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `POSTIE_ENABLED` | boolean | `false` | Enable Postie integration |
| `POSTIE_WATCH_DIR` | string | import watch dir | Directory where Postie writes NZBs |
| `POSTIE_TIMEOUT_MINUTES` | int | `120` | Minutes to wait before marking as failed |

### UI Configuration

All Postie settings can be configured through the AltMount web interface:

1. Navigate to **Configuration**
2. Select **SABnzbd API Configuration**
3. Scroll to **Postie Integration** section
4. Toggle and configure settings
5. Click **Save Changes**

## Troubleshooting

### Postie uploads are timing out

**Symptoms**: Queue items show `postie_upload_status: postie_failed`

**Solutions**:
1. Check if Postie is running: `docker logs postie`
2. Verify Postie can access SABnzbd's download directory: `docker exec postie ls /downloads/completed`
3. Check Postie server configuration (host, port, credentials)
4. Increase `timeout_minutes` in UI
5. Check Postie logs: `docker logs -f postie`

### NZBs aren't being matched to original items

**Symptoms**: Postie-generated NZBs appear as new queue items instead of re-imports

**Solutions**:
1. Verify Postie is using **partial obfuscation** (not full)
2. Check that `original_release_name` is saved in database:
   ```sql
   SELECT id, nzb_path, original_release_name, postie_upload_status
   FROM import_queue
   WHERE postie_upload_status IS NOT NULL;
   ```
3. Check AltMount logs for matching information
4. The matcher uses filename similarity, category, and file size

### "Fallback not configured" error

**Symptoms**: Cannot enable Postie integration in UI

**Solutions**:
1. Configure SABnzbd fallback first (Host and API Key)
2. Ensure SABnzbd is accessible from AltMount container
3. Test SABnzbd API: `curl http://sabnzbd:8080/api?mode=version`

### No NZBs appearing in watch directory

**Symptoms**: Postie completes but AltMount doesn't pick up NZBs

**Solutions**:
1. Verify volume mounts in docker-compose.yml
2. Check Postie's `nzb.output_dir` setting
3. Verify AltMount watcher is running (check logs)
4. Ensure watch directory paths match exactly

### Manual retry is needed

**Symptoms**: Postie upload failed and needs retry

**Solutions**:
1. Go to **Queue Management** in the web UI
2. Find the item with `postie_upload_status: postie_failed`
3. Click the **Retry Postie Upload** button in the actions menu

Or via API:
```bash
curl -X POST http://altmount:8080/api/queue/{id}/postie-retry
```

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/queue/postie/pending` | GET | List items waiting for Postie upload |
| `/api/queue/postie/failed` | GET | List items where Postie upload failed |
| `/api/queue/{id}/postie-retry` | POST | Manually retry a failed Postie upload |
| `/api/queue/postie/check-timeouts` | POST | Manually trigger timeout check |

## Feature Status Matrix

| Feature | Postie Disabled | Postie Enabled |
|---------|----------------|----------------|
| SABnzbd Fallback | ✅ Works | ✅ Works (required) |
| Postie Upload | ❌ N/A | ✅ Works |
| Re-import with new message IDs | ❌ N/A | ✅ Works |
| ARR Notification | ✅ Works | ✅ Works (same flow) |

## Related Documentation

- [ARR Integration](./integration.md) - How Sonarr/Radarr integration works
- [SABnzbd Configuration](../2.%20Installation/docker-volume-plugin.md) - SABnzbd setup guide
- [Import Strategy](./integration.md#import-strategy-configuration) - Symlinks, STRM, NONE options
