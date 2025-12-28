---
sidebar_position: 4
title: Integrations
---

# Integrations

AltMount integrates seamlessly with the ARR suite (Sonarr, Radarr, etc.) and various media servers.

## Import Strategy Configuration

When configuring AltMount, you can choose how imported files are made available to external applications. This is controlled by the "Import Strategy" setting.

### Import Strategies

#### 1. NONE (Direct WebDAV)

Use the WebDAV mount directly without additional file organization.

- **Best for**: Simple setups, setups where the media server can access WebDAV directly, or when using rclone mount to expose WebDAV as a filesystem.
- **How it works**: Files are accessed directly from the mount point.
- **Configuration**:
  - Select "NONE" in the configuration wizard.
  - Ensure your ARR applications and media server are pointing to the mount path.

#### 2. Symlinks

Creates category-based symbolic links for easier access by external applications.

- **Best for**: Plex, Emby, Jellyfin, and other media servers that run on the same system or have access to the same filesystem.
- **How it works**: AltMount creates symlinks in a specified directory (e.g., `/symlinks`) organized by category (e.g., `/symlinks/tv/Show Name/Season 1/Episode.mkv`). These symlinks point to the actual files in the WebDAV mount.
- **Configuration**:
  - Select "Symlinks" in the configuration wizard.
  - Set **Symlink Directory** to an absolute path.

#### 3. STRM

Generates STRM streaming files that point to the WebDAV content.

- **Best for**: Cloud-based setups, or when the media server (Emby, Jellyfin) supports STRM files and you want to avoid mounting the filesystem directly.
- **How it works**: AltMount creates `.strm` text files containing the HTTP URL of the media file. When the media server plays the STRM file, it streams the content directly from the URL.
- **Configuration**:
  - Select "STRM" in the configuration wizard.
  - Set **STRM Directory** to an absolute path.

## ARR Integration (Sonarr/Radarr)

AltMount acts as a download client for Sonarr and Radarr, mimicking the SABnzbd API.

### Setup

1.  **In AltMount**: Ensure the SABnzbd API is enabled in `Configuration -> SABnzbd`.
2.  **In Sonarr/Radarr**:
    -   Go to **Settings -> Download Clients**.
    -   Add a new **SABnzbd** client.
    -   **Host**: Your AltMount IP/Hostname.
    -   **Port**: `8080` (or your configured port).
    -   **API Key**: The API Key for the ARR instance (found in ARR Settings -> General).
    -   **Username**: The URL of your ARR instance (e.g., `http://sonarr:8989`).
    -   **Password**: The API Key for the ARR instance.
    -   **Category**: `tv` (for Sonarr) or `movies` (for Radarr).

> **Note**: AltMount uses the Username (URL) and Password (API Key) to register the ARR instance for callbacks. This allows AltMount to notify Sonarr/Radarr when a download is "complete" (i.e., imported).

### How it works

1.  Sonarr/Radarr sends an NZB to AltMount (acting as SABnzbd).
2.  AltMount adds the NZB to its internal queue.
3.  AltMount downloads the content via the configured NNTP providers.
4.  Once downloaded and processed, AltMount notifies the ARR instance that the download is complete.
5.  If using "Symlinks" or "STRM", the files are ready in the configured directory. If using "NONE", they are in the mount.

