# Fix: Memory and CPU Leaks After Extended Runtime

## Problem
After running for 6+ hours, the application experiences significant memory and CPU usage growth, indicating resource leaks.

## Root Causes Identified

### 1. **Goroutine Leaks**
- Untracked goroutines in background operations (health checks, VFS notifications)
- Goroutines not properly cancelled on service shutdown
- Missing WaitGroup tracking for background operations

### 2. **Memory Leaks**
- Unbounded map growth in progress broadcaster (subscribers map)
- Unbounded sync.Map growth in stream tracker
- Maps not cleaned up on service stop (activeChecks, cancelFuncs, downloadingSegments)
- Inefficient slice cleanup in metrics tracker

### 3. **Timer Leaks**
- `time.After()` called in loops creating multiple timers
- Tickers not properly stopped in all code paths

### 4. **Channel Leaks**
- Closed channels causing panics without recovery
- Channels not properly cleaned up on disconnect

## Changes Made

### `internal/progress/progress_broadcaster.go`
- Added context-based cleanup goroutine for stale subscribers
- Added panic recovery in `UpdateProgress()` to handle closed channels gracefully
- Clean up progress map in `Close()` method
- Prevent unbounded subscriber map growth

### `internal/api/stream_tracker.go`
- Added cleanup goroutine to remove streams active > 1 hour (abandoned streams)
- Added `Close()` method for proper resource cleanup
- Prevent unbounded sync.Map growth

### `internal/usenet/usenet_reader.go`
- Clean up `downloadingSegments` map in `Close()` method
- Replaced `time.After()` in download loop with single ticker to prevent timer leaks
- Proper cleanup of segment tracking maps

### `internal/health/worker.go`
- Cancel all active checks in `Stop()` method
- Track background check goroutines with WaitGroup
- Clean up `activeChecks` map on shutdown

### `internal/health/checker.go`
- Added panic recovery in `notifyRcloneVFS()` goroutine
- Added timeout context to prevent hanging operations

### `internal/importer/service.go`
- Keep `notifyRcloneVFS()` synchronous with timeout (must complete before ARR scan)
- Clean up `cancelFuncs` map in `Stop()` method
- Ensure rclone VFS refresh completes before triggering sonarr/radarr scan

### `internal/nzbfilesystem/metadata_remote_file.go`
- Added panic recovery in `updateFileHealthOnError()` goroutines
- Added timeout contexts (30 seconds) to prevent hanging

### `pkg/rclonecli/health.go`
- Added panic recovery in mount recovery goroutines
- Added 2-minute timeout context

### `internal/pool/metrics_tracker.go`
- Improved `cleanupOldSamples()` to reallocate slice when removing >50% of samples
- More efficient memory management for metrics history

## Testing
- All changes maintain existing functionality
- No changes to core application logic
- Only resource cleanup and leak prevention added
- Proper context cancellation and timeout handling

## Impact
- Prevents memory growth over extended runtime
- Reduces CPU usage from leaked goroutines
- Improves application stability
- No breaking changes to API or functionality
