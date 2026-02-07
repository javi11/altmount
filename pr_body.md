### Summary
This PR implements critical safety enhancements for the health monitoring system and corrects path reporting in the SABnzbd API to ensure stability and prevent data loss.

### Key Changes

#### 1. Path Reporting & SABnzbd API Fixes
- **Corrected Path Reporting:** Implemented a new `JoinAbsPath` helper to safely join mount paths with subdirectories. This fixes a bug where absolute paths in configuration caused doubled prefixes (e.g., `/mnt/usenet/mnt/usenet/...`), which led to \"directory does not exist\" warnings in Sonarr/Radarr.
- **Accurate Disk Space:** Updated the SABnzbd status API to report free space from the actual `mount_path` instead of the temporary upload directory.
- **Rclone Connectivity:** Switched Rclone RC connections to explicitly use `127.0.0.1` instead of `localhost` to avoid IPv6 (`::1`) connection failures in Docker environments.

#### 2. Critical Safety Enhancements (\"Triple-Lock\" System)
- **Mount Protection:** Added logic to automatically abort all cleanup operations if a library scan returns zero files while metadata exists. This prevents mass deletion of metadata and database records during temporary mount failures or network glitches.
- **Physical File Lock:** Explicitly blocked `os.Remove()` calls for media files when using the **NONE** import strategy. This makes it physically impossible for AltMount to delete actual media from the mount point during cleanup.
- **Strict Type-Checking:** Implemented validation to ensure only symlinks are deleted in SYMLINK mode and only `.strm` files in STRM mode.

#### 3. Health System Improvements
- **Universal Library Sync:** Enabled full library scanning for the **NONE** import strategy if a Library Directory is configured. This allows AltMount to track and follow files moved or renamed by Sonarr/Radarr within the mount.
- **Auto-Path Updates:** Implemented automatic health record updates when a file is moved via WebDAV (MOVE operation), eliminating the need to wait for a periodic sync.
- **State Persistence:** Added a `system_state` table to persist library sync results across restarts.
- **Database Stability:** Made `last_checked` nullable to prevent SQL scan errors when resetting health records.

#### 4. UI/UX
- Updated the **Health & Repair** settings UI to clarify the use of the Library Directory for the NONE strategy and removed misleading notes.
- Switched to **relative symlinks** for better portability across different container mount points.

### Verification Results
- Added unit tests for path joining logic (`TestToSABnzbdHistorySlot`).
- Verified health system worker pool and cleanup logic with existing test suites.
- Confirmed stability in a production-like environment with 2,600+ files.
