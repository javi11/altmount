# Streaming Configuration

AltMount's streaming system enables direct media playback from Usenet without waiting for complete downloads. It uses intelligent prefetching to keep playback smooth.

## Configuration

Configure the streaming prefetch through the System Configuration interface:

![Streaming Configuration](/images/config-streaming.png)
_Streaming settings in the system configuration_

### Parameters

| Parameter      | Description                                           | Default |
| -------------- | ----------------------------------------------------- | ------- |
| `max_prefetch` | Number of segments to prefetch ahead during streaming | `30`    |

```yaml
streaming:
  max_prefetch: 30 # Number of segments prefetched ahead (default: 30)
```

Higher values improve playback smoothness for high-bitrate content but increase memory usage. Lower values are better for resource-constrained environments.

## FUSE Mount Recommended Settings

If you use AltMount's built-in FUSE mount (`mount_type: fuse`), tuning the FUSE and VFS disk cache settings is critical for smooth streaming playback. The built-in FUSE mount avoids the need for an external rclone process and provides an integrated caching layer with intelligent prefetching.

```yaml
mount_type: fuse
mount_path: /mnt/altmount

fuse:
  allow_other: true
  attr_timeout_seconds: 30
  entry_timeout_seconds: 1
  max_cache_size_mb: 128
  max_read_ahead_mb: 128

segment_cache:
  enabled: true
  cache_path: /mnt/cache/altmount-segcache
  max_size_gb: 150
  expiry_hours: 72
```

### Parameter Reference

#### FUSE Mount Options

| Parameter               | Default                   | Description                                                                                          |
| ----------------------- | ------------------------- | ---------------------------------------------------------------------------------------------------- |
| `mount_path`            | —                         | Mount point path (synced automatically from the top-level `mount_path` when `mount_type` is `fuse`) |
| `enabled`               | `false`                   | Whether the FUSE mount is active (set automatically based on `mount_type` — no need to set manually) |
| `allow_other`           | `true`                    | Allows other users (e.g., media players) to access the mount                                         |
| `debug`                 | `false`                   | Enables FUSE debug logging (very verbose — use only for troubleshooting)                             |
| `attr_timeout_seconds`  | `30`                      | How long the kernel caches file attributes (size, timestamps)                                        |
| `entry_timeout_seconds` | `1`                       | How long the kernel caches directory entries — lower values refresh faster                            |
| `max_cache_size_mb`     | `128`                     | Maximum kernel-level cache size in MB                                                                |
| `max_read_ahead_mb`     | `128`                     | Kernel-level read-ahead buffer size in MB                                                            |

#### Segment Cache Options

The segment cache provides a persistent on-disk caching layer shared by both FUSE and WebDAV. Each cached entry corresponds to one decoded Usenet article (~750 KB). Enabling it is **strongly recommended** for media playback.

| Parameter      | Default                    | Description                                                      |
| -------------- | -------------------------- | ---------------------------------------------------------------- |
| `enabled`      | `false`                    | Enables the segment cache — **set to `true` for streaming**      |
| `cache_path`   | `/tmp/altmount-segcache`   | Directory for cached data (use a fast disk for best results)     |
| `max_size_gb`  | `10`                       | Maximum disk space for the cache (adjust to your available disk) |
| `expiry_hours` | `24`                       | How long cached segments are kept before eviction                |

### How the Segment Cache Works

When `enabled` is `true`, reads flow through a two-tier caching system:

1. **Cache hit** — Data is served directly from the local disk cache with no network round-trip.
2. **Cache miss** — The requested segment is fetched from the backend, written to the disk cache, and returned to the reader.

Cache eviction runs automatically every 5 minutes, removing expired entries and enforcing the size limit via LRU (least recently used). Files that are currently open are never evicted.

### Tips

- **Enable segment cache** for media playback. Without it, every read goes directly to the backend with no local caching, which will cause buffering.
- **Use a fast disk** for `cache_path`. An SSD or NVMe drive significantly improves cache read performance. Avoid placing the cache on the same slow storage you're mounting.
- Increase `max_size_gb` based on your available disk space. For large libraries, `50`–`150` GB prevents frequent cache evictions during heavy usage.
- Increase `expiry_hours` to `72` if you re-watch content frequently — this keeps popular segments cached longer.
- **`allow_other: true`** is required if media players run as a different user than the AltMount process.
- Keep `attr_timeout_seconds` at `30` for stable libraries. Lower it (e.g., `5`) if files change frequently and you need faster metadata refresh.

---

## Rclone VFS Recommended Settings

If you use rclone to mount AltMount's WebDAV endpoint, tuning VFS settings is critical for smooth playback. Below are community-tested recommendations:

```bash
rclone mount altmount: /mnt/remotes/altmount \
  --vfs-cache-mode full \
  --vfs-read-chunk-size 56M \
  --vfs-cache-max-size 150G \
  --vfs-cache-max-age 72h \
  --vfs-read-ahead 80G \
  --vfs-cache-min-free-space 1G \
  --vfs-read-chunk-streams 4 \
  --read-chunk-size 32M \
  --read-chunk-size-limit 2G \
  --dir-cache-time 5s \
  --buffer-size 0 \
  --allow-other
```

### Parameter Reference

| Parameter                    | Recommended | Description                                                               |
| ---------------------------- | ----------- | ------------------------------------------------------------------------- |
| `--vfs-cache-mode`           | `full`      | Caches full files locally for smooth playback                             |
| `--vfs-read-chunk-size`      | `56M`       | Initial chunk size for reads — larger reduces round-trips                 |
| `--vfs-cache-max-size`       | `150G`      | Maximum disk space for the VFS cache (adjust to your available disk)      |
| `--vfs-cache-max-age`        | `72h`       | How long cached files are kept before eviction                            |
| `--vfs-read-ahead`           | `80G`       | Amount of data to read ahead — higher values reduce buffering             |
| `--vfs-cache-min-free-space` | `1G`        | Minimum free disk space to maintain                                       |
| `--vfs-read-chunk-streams`   | `4`         | Number of parallel streams per file read                                  |
| `--read-chunk-size`          | `32M`       | Backend chunk size for reads                                              |
| `--read-chunk-size-limit`    | `2G`        | Maximum chunk size (rclone scales up to this)                             |
| `--dir-cache-time`           | `5s`        | How long directory listings are cached — lower values mean faster refresh |
| `--buffer-size`              | `0`         | In-memory buffer size (0 lets VFS cache handle it)                        |

### Tips

- **`--vfs-cache-mode full`** is strongly recommended for media playback. Without it, seeking and multi-stream playback (e.g., subtitles) may fail.
- Adjust `--vfs-cache-max-size` based on your available disk space. For smaller drives, `50G` works but may cause more cache evictions during heavy usage.
- If playback still freezes, try increasing `--vfs-read-chunk-size` and `--vfs-read-ahead`.
- Use `--allow-other` if media players run as a different user than the rclone mount process.

## Next Steps

With streaming configured:

1. **[Set up Health Monitoring](../3.%20Configuration/health-monitoring.md)** - Monitor file integrity

---

For advanced streaming scenarios and troubleshooting, see the [Troubleshooting Guide](../5.%20Troubleshooting/performance.md).
