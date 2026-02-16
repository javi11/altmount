# Performance Optimization

This guide covers performance tuning strategies for AltMount to maximize download speeds, optimize resource usage, and provide the best streaming experience for your specific environment.

## Performance Assessment

### Baseline Testing

Before optimizing, establish performance baselines:

#### System Performance Test

```bash
# Test disk I/O performance
dd if=/dev/zero of=/tmp/testfile bs=1G count=1 oflag=direct

# Test network bandwidth to provider
wget --user=username --password=password \
     --output-document=/dev/null \
     ftp://provider.com/test-100mb.bin

# Test system resources
htop
iotop -o
```

#### AltMount Performance Metrics

```bash
# Get current performance stats
curl -u user:pass http://localhost:8080/api/queue/stats

# Monitor worker utilization
curl -u user:pass http://localhost:8080/api/health/streaming

# Check provider performance
curl -u user:pass http://localhost:8080/api/providers
```

_[Screenshot placeholder: Performance monitoring dashboard showing baseline metrics and resource utilization]_

### Performance Goals

Set realistic performance targets based on your environment:

| Environment     | Target Download Speed | Target Response Time | Memory Usage |
| --------------- | --------------------- | -------------------- | ------------ |
| Home Server     | 50-100 Mbps           | &lt;200ms            | &lt;512MB    |
| High-End Server | 200-500 Mbps          | &lt;100ms            | &lt;1GB      |
| Enterprise      | 500+ Mbps             | &lt;50ms             | &lt;2GB      |
| Constrained     | 10-50 Mbps            | &lt;500ms            | &lt;256MB    |

## System-Level Optimizations

### Hardware Optimization

#### Storage Configuration

**Metadata Storage** (Critical for performance):

```yaml
metadata:
  root_path: "/ssd/altmount/metadata" # Use SSD for metadata
```

**Storage Performance Impact**:

- **SSD**: 10-50x faster metadata operations
- **NVMe**: Additional 2-3x improvement over SATA SSD
- **Network Storage**: Significant performance penalty

**Recommended Storage Layout**:

```bash
# Optimal setup
/ssd/altmount/metadata/     # Fast SSD for metadata
/hdd/altmount/cache/        # Large HDD for download cache (optional)
/tmp/altmount/temp/         # RAM disk for temporary operations (advanced)
```

_[Screenshot placeholder: Storage performance comparison showing SSD vs HDD metadata access times]_

#### Memory Configuration

**System Memory Requirements**:

```yaml
streaming:
  max_range_size: 67108864 # 64MB per range request
  max_download_workers: 25 # ~25MB per worker
  # Total memory estimate: (range_size * active_ranges) + (worker_count * 25MB)
```

**Memory Optimization**:

```bash
# Linux: Increase network buffers
echo 'net.core.rmem_max = 16777216' >> /etc/sysctl.conf
echo 'net.core.wmem_max = 16777216' >> /etc/sysctl.conf
echo 'net.core.netdev_max_backlog = 5000' >> /etc/sysctl.conf

# Apply settings
sysctl -p
```

#### Network Optimization

**TCP Tuning**:

```bash
# Optimize for high bandwidth, high latency
echo 'net.ipv4.tcp_window_scaling = 1' >> /etc/sysctl.conf
echo 'net.ipv4.tcp_rmem = 4096 32768 16777216' >> /etc/sysctl.conf
echo 'net.ipv4.tcp_wmem = 4096 32768 16777216' >> /etc/sysctl.conf
echo 'net.ipv4.tcp_congestion_control = bbr' >> /etc/sysctl.conf
```

**DNS Optimization**:

```bash
# Use fast DNS servers
echo 'nameserver 1.1.1.1' > /etc/resolv.conf
echo 'nameserver 8.8.8.8' >> /etc/resolv.conf
```

### Operating System Tuning

#### File Descriptor Limits

```bash
# Increase file descriptor limits
echo 'altmount soft nofile 65536' >> /etc/security/limits.conf
echo 'altmount hard nofile 65536' >> /etc/security/limits.conf

# For systemd services
mkdir -p /etc/systemd/system/altmount.service.d/
cat > /etc/systemd/system/altmount.service.d/limits.conf << EOF
[Service]
LimitNOFILE=65536
EOF
```

#### Process Priority

```bash
# Run AltMount with higher priority
nice -n -10 ./altmount serve --config=config.yaml

# For systemd service
echo 'Nice=-10' >> /etc/systemd/system/altmount.service
```

_[Screenshot placeholder: System monitoring showing optimized resource allocation and process priorities]_

## AltMount Configuration Optimization

### Streaming Performance Tuning

#### High-Performance Configuration

```yaml
streaming:
  max_range_size: 134217728 # 128MB - Large ranges for high bandwidth
  streaming_chunk_size: 33554432 # 32MB - Large chunks for throughput
  max_download_workers: 35 # High worker count for fast systems

import:
  max_processor_workers: 4 # Multiple NZB processors
  queue_processing_interval_seconds: 2 # Fast queue processing
```

#### Balanced Configuration

```yaml
streaming:
  max_range_size: 67108864 # 64MB - Good balance
  streaming_chunk_size: 16777216 # 16MB - Medium chunks
  max_download_workers: 25 # Moderate worker count

import:
  max_processor_workers: 2 # Standard processing
  queue_processing_interval_seconds: 5 # Standard interval
```

#### Resource-Constrained Configuration

```yaml
streaming:
  max_range_size: 16777216 # 16MB - Smaller ranges
  streaming_chunk_size: 4194304 # 4MB - Small chunks
  max_download_workers: 12 # Conservative worker count

import:
  max_processor_workers: 1 # Single processor
  queue_processing_interval_seconds: 10 # Slower processing
```

### Provider Optimization

#### Connection Optimization

```yaml
providers:
  # Primary provider with maximum connections
  - name: "primary-fast"
    host: "fastest-endpoint.provider.com"
    port: 563
    max_connections: 50 # Maximum allowed by provider
    tls: true

  # Backup provider for load balancing
  - name: "backup-provider"
    host: "backup.provider.com"
    port: 563
    max_connections: 30
    tls: true
```

#### Multi-Provider Strategy

```yaml
providers:
  # Tier 1 provider - highest performance
  - name: "tier1-primary"
    host: "premium.provider.com"
    max_connections: 40

  # Tier 2 provider - different backbone
  - name: "tier2-backup"
    host: "alternative.provider.com"
    max_connections: 30

  # Block provider for fill-in
  - name: "block-provider"
    host: "block.provider.com"
    max_connections: 15
```

### Database Optimization

#### SQLite Performance Tuning

```bash
# Optimize SQLite database
sqlite3 altmount.db << EOF
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA cache_size = 10000;
PRAGMA temp_store = memory;
PRAGMA mmap_size = 268435456;
EOF
```

#### Database Maintenance

```bash
# Regular database optimization
sqlite3 altmount.db "VACUUM;"
sqlite3 altmount.db "ANALYZE;"

# Schedule weekly via cron
echo "0 2 * * 0 sqlite3 /path/to/altmount.db 'VACUUM; ANALYZE;'" | crontab -
```

## Workload-Specific Optimizations

### 4K Media Streaming

#### Configuration for 4K Content

```yaml
streaming:
  max_range_size: 268435456 # 256MB - Large buffers for 4K
  streaming_chunk_size: 67108864 # 64MB - Big chunks
  max_download_workers: 40 # High concurrency

webdav:
  debug: false # Disable debug for performance

metadata:
  root_path: "/nvme/altmount/metadata" # NVMe storage

# High-performance logging
log:
  level: "warn" # Minimal logging
  max_size: 200
  compress: true
```

**System Requirements for 4K**:

- **CPU**: 8+ cores, 3+ GHz
- **Memory**: 16+ GB RAM
- **Storage**: NVMe SSD for metadata
- **Network**: 200+ Mbps internet, Gigabit LAN
- **Providers**: Premium Usenet providers with high connection limits

_[Screenshot placeholder: 4K streaming performance dashboard showing high bandwidth utilization and smooth playback metrics]_

### Multiple Concurrent Users

#### Multi-User Configuration

```yaml
streaming:
  max_range_size: 67108864 # 64MB - Balanced for multiple streams
  streaming_chunk_size: 16777216 # 16MB - Medium chunks
  max_download_workers: 60 # High total workers for concurrency

webdav:
  port: 8080
  # Consider load balancer for very high concurrency

# Multiple high-performance providers
providers:
  - name: "primary"
    max_connections: 50
  - name: "secondary"
    max_connections: 40
  - name: "tertiary"
    max_connections: 30
```

**Scaling Guidelines**:

- **2-3 users**: 20-25 workers, 64MB range size
- **4-6 users**: 35-45 workers, 64MB range size
- **7-10 users**: 50-60 workers, 32MB range size
- **10+ users**: Consider load balancing multiple instances

### High-Volume Downloads

#### Bulk Download Optimization

```yaml
streaming:
  max_range_size: 134217728 # 128MB - Large ranges
  streaming_chunk_size: 33554432 # 32MB - Large chunks
  max_download_workers: 50 # Maximum workers

import:
  max_processor_workers: 8 # Fast NZB processing
  queue_processing_interval_seconds: 1 # Very fast queue processing

# Optimize for throughput over latency
log:
  level: "error" # Minimal logging overhead
```

## Monitoring and Benchmarking

### Performance Metrics

#### Key Performance Indicators (KPIs)

```bash
# Download speed
curl -u user:pass http://localhost:8080/api/queue/stats | jq '.performance.avg_speed'

# Worker utilization
curl -u user:pass http://localhost:8080/api/health/streaming | jq '.workers.utilization'

# Response time
curl -w "@curl-format.txt" http://localhost:8080/api/health

# Provider performance
curl -u user:pass http://localhost:8080/api/providers | jq '.providers[].statistics'
```

#### Continuous Monitoring Script

```bash
#!/bin/bash
# Performance monitoring script

ALTMOUNT_URL="http://localhost:8080"
AUTH="user:pass"

while true; do
    timestamp=$(date '+%Y-%m-%d %H:%M:%S')

    # Get performance stats
    stats=$(curl -s -u "$AUTH" "$ALTMOUNT_URL/api/queue/stats")
    speed=$(echo "$stats" | jq -r '.performance.avg_speed // "0"')
    workers=$(echo "$stats" | jq -r '.workers.utilization // "0"')

    # Get health info
    health=$(curl -s -u "$AUTH" "$ALTMOUNT_URL/api/health")
    status=$(echo "$health" | jq -r '.status // "unknown"')

    # Log metrics
    echo "$timestamp,$speed,$workers,$status" >> /var/log/altmount-performance.csv

    sleep 30
done
```

_[Screenshot placeholder: Performance monitoring dashboard with real-time graphs showing speed, utilization, and health metrics]_

### Benchmarking Tools

#### Internal Benchmarks

```bash
# Test WebDAV performance
curl -w "@curl-format.txt" -u user:pass http://localhost:8080/large-file.mkv

# Test API performance
ab -n 100 -c 10 -A user:pass http://localhost:8080/api/health

# Test streaming performance
ffmpeg -i http://user:pass@localhost:8080/test-video.mkv -f null -
```

#### External Benchmarks

```bash
# Test provider direct download speed
wget --user=provider_user --password=provider_pass \
     --output-document=/dev/null \
     ftp://provider.com/speedtest-1gb.bin

# Compare with AltMount download speed
curl -u altmount_user:altmount_pass \
     http://localhost:8080/speedtest-file.bin > /dev/null
```

## Performance Troubleshooting

### Identifying Bottlenecks

#### System Resource Analysis

```bash
# CPU bottlenecks
top -p $(pgrep altmount)

# Memory bottlenecks
ps aux | grep altmount | awk '{print $4}'

# Disk I/O bottlenecks
iotop -p $(pgrep altmount)

# Network bottlenecks
iftop -i eth0
```

#### AltMount-Specific Analysis

```bash
# Worker saturation
curl -u user:pass http://localhost:8080/api/health/streaming | jq '.workers'

# Provider performance
curl -u user:pass http://localhost:8080/api/providers | jq '.providers[].statistics'

# Queue backlog
curl -u user:pass http://localhost:8080/api/queue | jq '.summary'
```

_[Screenshot placeholder: System resource monitoring showing bottleneck identification and resource utilization patterns]_

### Common Performance Issues

#### Slow Download Speeds

**Diagnosis**:

```bash
# Check worker utilization (should be 80-95%)
curl -u user:pass http://localhost:8080/api/health/streaming | jq '.workers.utilization'

# Check provider connection usage
curl -u user:pass http://localhost:8080/api/providers | jq '.providers[].connections'

# Test individual provider speeds
curl -u user:pass http://localhost:8080/api/providers/provider-name/test
```

**Solutions**:

1. **Increase Workers**: If utilization is low, increase `max_download_workers`
2. **Add Providers**: Distribute load across more providers
3. **Optimize Chunks**: Increase chunk sizes for better throughput
4. **Check Network**: Verify network isn't the bottleneck

#### High Memory Usage

**Diagnosis**:

```bash
# Monitor memory usage over time
while true; do
  ps -o pid,ppid,cmd,%mem,%cpu -p $(pgrep altmount)
  sleep 5
done
```

**Solutions**:

1. **Reduce Range Size**: Lower `max_range_size` to use less memory per request
2. **Limit Workers**: Reduce `max_download_workers` if memory constrained
3. **Check for Leaks**: Monitor for gradual memory increase over time

#### Poor Streaming Performance

**Diagnosis**:

```bash
# Test streaming latency
curl -w "@curl-format.txt" -r 0-1048575 -u user:pass \
     http://localhost:8080/large-video.mkv > /dev/null

# Check concurrent stream performance
for i in {1..5}; do
  curl -r $((i*1048576))-$(((i+1)*1048576-1)) -u user:pass \
       http://localhost:8080/test-video.mkv > /dev/null &
done
```

**Solutions**:

1. **Optimize for Streaming**: Use smaller chunks for lower latency
2. **Increase Workers**: Ensure enough workers for concurrent streams
3. **Provider Selection**: Use fastest providers for streaming content

## Advanced Performance Techniques

### Caching Strategies

#### Operating System Cache

```bash
# Increase system cache for frequently accessed files
echo 'vm.vfs_cache_pressure = 50' >> /etc/sysctl.conf
echo 'vm.dirty_ratio = 15' >> /etc/sysctl.conf
echo 'vm.dirty_background_ratio = 5' >> /etc/sysctl.conf
```

#### Application-Level Caching

```yaml
# Use fast storage for temporary operations
metadata:
  root_path: "/ssd/altmount/metadata"
# Consider RAM disk for very high performance (advanced)
# metadata:
#   root_path: "/tmpfs/altmount/metadata"
```

### Load Balancing

#### Multiple AltMount Instances

```yaml
# Instance 1
webdav:
  port: 8080

streaming:
  max_download_workers: 20

# Instance 2
webdav:
  port: 8081

streaming:
  max_download_workers: 20
```

**HAProxy Configuration**:

```
backend altmount
    balance roundrobin
    server altmount1 127.0.0.1:8080 check
    server altmount2 127.0.0.1:8081 check
```

_[Screenshot placeholder: Load balancing dashboard showing traffic distribution across multiple AltMount instances]_

### Provider Optimization

#### Connection Pool Management

```yaml
providers:
  - name: "optimized-provider"
    host: "provider.com"
    port: 563
    max_connections: 50 # Optimal based on testing
    tls: true

    # Advanced: Connection persistence (if supported)
    # keep_alive: true
    # connection_timeout: 30
```

#### Geographic Optimization

```yaml
providers:
  # Use geographically closer providers when possible
  - name: "us-provider"
    host: "us-news.provider.com"
    max_connections: 30

  - name: "eu-provider"
    host: "eu-news.provider.com"
    max_connections: 30
```

## Performance Testing

### Load Testing

#### Concurrent User Simulation

```bash
#!/bin/bash
# Simulate multiple concurrent users

for i in {1..10}; do
  {
    curl -u user$i:pass$i -r 0-104857600 \
         http://localhost:8080/test-file-100mb.bin > /dev/null
    echo "User $i completed"
  } &
done

wait
echo "All users completed"
```

#### Sustained Load Testing

```bash
# Test sustained performance over time
ab -n 1000 -c 50 -t 300 -A user:pass \
   http://localhost:8080/api/health
```

### Performance Regression Testing

#### Automated Performance Tests

```bash
#!/bin/bash
# Performance regression test script

# Baseline performance test
baseline_speed=$(curl -s -u user:pass http://localhost:8080/api/queue/stats | jq -r '.performance.avg_speed')

# Alert if performance drops below threshold
threshold_speed="20.0 MB/s"
if (( $(echo "$baseline_speed < $threshold_speed" | bc -l) )); then
    echo "ALERT: Performance regression detected"
    echo "Current speed: $baseline_speed"
    echo "Threshold: $threshold_speed"
fi
```

## Best Practices Summary

### Configuration Best Practices

1. **Start Conservative**: Begin with default settings and optimize based on monitoring
2. **Monitor First**: Always monitor before and after changes to measure impact
3. **Test Incrementally**: Make one change at a time to identify what works
4. **Document Changes**: Keep track of configuration changes and their effects

### Hardware Recommendations

1. **Storage**: Use SSD for metadata storage, consider NVMe for extreme performance
2. **Memory**: 2-4GB RAM for most installations, more for high-concurrency scenarios
3. **CPU**: Multi-core processors benefit worker parallelization
4. **Network**: Ensure network bandwidth exceeds provider limits

### Monitoring Strategy

1. **Baseline Metrics**: Establish baseline performance before optimization
2. **Continuous Monitoring**: Monitor key metrics continuously in production
3. **Alerting**: Set up alerts for performance degradation
4. **Regular Review**: Periodically review and adjust optimization settings

### Scaling Guidelines

1. **Vertical Scaling**: Optimize single instance first (CPU, memory, storage)
2. **Horizontal Scaling**: Add multiple instances for very high loads
3. **Provider Scaling**: Add more providers before scaling infrastructure
4. **Client Scaling**: Consider WebDAV client optimization for better performance

---

## Next Steps

With performance optimized:

1. **[Health Monitoring](../3. Configuration/health-monitoring.md)** - Set up performance monitoring
2. **[Common Issues](common-issues.md)** - Troubleshoot any remaining issues

Remember: Performance optimization is an iterative process. Monitor, measure, adjust, and repeat to achieve optimal results for your specific environment.
