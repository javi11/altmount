# Performance Optimization

Tips for tuning AltMount to maximize download speeds and streaming quality.

## Storage

Use an SSD or NVMe drive for metadata storage. This is the single biggest performance improvement you can make — SSD provides 10-50x faster metadata operations compared to spinning disks, and NVMe adds another 2-3x on top of that.

```yaml
metadata:
  root_path: "/ssd/altmount/metadata" # Use SSD/NVMe for metadata
```

## AltMount Configuration Optimization

### Streaming Performance Tuning

The streaming system has a single tuning parameter: `max_prefetch`, which controls how many segments are prefetched ahead during streaming.

#### High-Performance Configuration

```yaml
streaming:
  max_prefetch: 60 # More prefetch for high-bandwidth setups

import:
  max_processor_workers: 4 # Multiple NZB processors
  queue_processing_interval_seconds: 2 # Fast queue processing
```

#### Balanced Configuration

```yaml
streaming:
  max_prefetch: 30 # Default — good balance

import:
  max_processor_workers: 2 # Standard processing
  queue_processing_interval_seconds: 5 # Standard interval
```

#### Resource-Constrained Configuration

```yaml
streaming:
  max_prefetch: 10 # Lower prefetch to save memory

import:
  max_processor_workers: 1 # Single processor
  queue_processing_interval_seconds: 10 # Slower processing
```

### Provider Optimization

#### Connection Optimization

```yaml
providers:
  # Primary provider with maximum connections
  - host: "fastest-endpoint.provider.com"
    port: 563
    max_connections: 50 # Maximum allowed by provider
    tls: true

  # Backup provider for load balancing
  - host: "backup.provider.com"
    port: 563
    max_connections: 30
    tls: true
    is_backup_provider: true
```

#### Multi-Provider Strategy

```yaml
providers:
  # Tier 1 provider - highest performance
  - host: "premium.provider.com"
    port: 563
    max_connections: 40
    tls: true

  # Tier 2 provider - different backbone
  - host: "alternative.provider.com"
    port: 563
    max_connections: 30
    tls: true

  # Block provider for fill-in
  - host: "block.provider.com"
    port: 563
    max_connections: 15
    tls: true
    is_backup_provider: true
```

## Common Performance Issues

### Playback Freezing

If media playback freezes or buffers frequently:

**Check these first:**

1. **Update to the latest AltMount version**: Playback improvements are shipped regularly. Pull the latest image:

   ```bash
   docker pull javi11/altmount:latest
   docker restart altmount
   ```

2. **Increase prefetch**: A low `max_prefetch` value can cause buffering on high-bitrate content:

   ```yaml
   streaming:
     max_prefetch: 45 # Increase from default 30
   ```

3. **Tune rclone VFS settings** (if using rclone mount): Playback freezing is often caused by rclone VFS settings rather than AltMount itself. See the [Streaming Configuration rclone section](../3.%20Configuration/streaming.md#rclone-vfs-recommended-settings) for recommended settings. Key parameters:
   - Set `--vfs-cache-mode full` (required for smooth playback)
   - Increase `--vfs-read-chunk-size` to `56M` or higher
   - Increase `--vfs-read-ahead` to `80G`

4. **Check for corruption**: Playback freezing on specific files may indicate corruption. AltMount detects corruption during playback and flags the file for health checking. Check the health dashboard to see if the file has been flagged.

5. **Provider speed**: If freezing affects all content, the issue may be provider bandwidth. Try:
   - Adding a second provider for load distribution
   - Increasing `max_connections` on your primary provider
   - Using a provider endpoint closer to your location

### Slow Download Speeds

**Solutions**:

1. **Increase Prefetch**: Raise `streaming.max_prefetch` for smoother streaming
2. **Add Providers**: Distribute load across more providers
3. **Increase Connections**: Raise `max_connections` per provider if allowed

### High Memory Usage

**Solutions**:

1. **Reduce Prefetch**: Lower `streaming.max_prefetch` to use less memory
2. **Limit Import Workers**: Reduce `import.max_processor_workers`
3. **Check for Leaks**: Monitor for gradual memory increase over time

### Poor Streaming Performance

**Solutions**:

1. **Increase Prefetch**: Higher `max_prefetch` helps with buffering
2. **Provider Selection**: Use fastest providers for streaming content
3. **Use SSD for Metadata**: Ensures fast file lookups

---

## Next Steps

With performance optimized:

1. **[Health Monitoring](../3.%20Configuration/health-monitoring.md)** - Set up performance monitoring
2. **[Common Issues](common-issues.md)** - Troubleshoot any remaining issues
