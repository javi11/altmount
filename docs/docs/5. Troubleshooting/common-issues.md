# Common Issues and Solutions

This guide covers the most frequently encountered issues with AltMount and their solutions, organized by category for quick diagnosis and resolution.

## Installation Issues

### Binary Won't Start

#### Symptoms

```
./altmount: permission denied
```

or

```
./altmount: cannot execute binary file
```

**Solutions:**

1. **Fix Permissions**:

   ```bash
   chmod +x altmount
   ```

2. **Check Architecture**: Ensure you downloaded the correct binary for your system:

   ```bash
   # Check your architecture
   uname -m

   # x86_64 -> use amd64 binary
   # aarch64 -> use arm64 binary
   ```

3. **Verify File Integrity**:

   ```bash
   # Check if file is corrupted
   file altmount

   # Should show: ELF 64-bit LSB executable...
   ```

_[Screenshot placeholder: Terminal showing successful binary execution with correct permissions]_

### Configuration File Not Found

#### Symptoms

```
Error: config file "config.yaml" not found
```

**Solutions:**

1. **Specify Config Path**:

   ```bash
   ./altmount serve --config=/full/path/to/config.yaml
   ```

2. **Create Config from Sample**:

   ```bash
   # Download sample configuration
   wget https://raw.githubusercontent.com/javi11/altmount/main/config.sample.yaml -O config.yaml

   # Edit with your settings
   nano config.yaml
   ```

3. **Check Working Directory**:
   ```bash
   # Run from directory containing config.yaml
   cd /path/to/altmount/config
   ./altmount serve
   ```

### Docker Container Won't Start

#### Symptoms

```
docker: Error response from daemon: invalid mount config
```

**Solutions:**

1. **Create Required Directories**:

   ```bash
   mkdir -p ./config ./metadata
   ```

2. **Fix Volume Permissions**:

   ```bash
   # Set correct ownership
   sudo chown -R 1000:1000 ./config ./metadata
   ```

3. **Check Docker Compose**:
   ```yaml
   # Ensure proper volume syntax
   volumes:
     - ./config:/config
     - ./metadata:/metadata
   ```

_[Screenshot placeholder: Docker logs showing successful container startup]_

## Connection Issues

### NNTP Provider Connection Failed

#### Symptoms

```
ERROR Provider "primary": connection failed: dial tcp: i/o timeout
```

**Diagnosis Steps:**

1. **Test Network Connectivity**:

   ```bash
   # Test provider reachability
   telnet ssl-news.provider.com 563

   # Check DNS resolution
   nslookup ssl-news.provider.com
   ```

2. **Verify Credentials**:

   ```bash
   # Check provider account status on their website
   # Ensure username/password are correct in config
   ```

3. **Check Firewall/ISP Blocking**:
   ```bash
   # Test different ports
   telnet provider.com 119  # Try non-SSL port
   telnet provider.com 563  # Try SSL port
   ```

**Solutions:**

1. **Correct Provider Settings**:

   ```yaml
   providers:
     - host: "correct-hostname.provider.com" # Verify hostname
       port: 563 # Use correct port
       username: "correct_username" # Check case sensitivity
       password: "correct_password" # Check special characters
       tls: true # Enable SSL if supported
   ```

2. **Try Alternative Endpoints**:

   ```yaml
   # Many providers offer multiple endpoints
   host: "ssl-eu.provider.com"     # European endpoint
   # or
   host: "ssl-us.provider.com"     # US endpoint
   ```

3. **Adjust Connection Limits**:
   ```yaml
   providers:
     - host: "provider.com"
       max_connections: 10 # Reduce if getting rejected
       # ... other settings
   ```

_[Screenshot placeholder: AltMount logs showing successful provider connections after troubleshooting]_

### WebDAV Cannot Be Accessed

#### Symptoms

- Browser shows "This site can't be reached"
- WebDAV clients show connection errors
- curl fails with connection refused

**Diagnosis:**

1. **Check AltMount Status**:

   ```bash
   # Verify AltMount is running
   ps aux | grep altmount

   # Check if port is listening
   netstat -ln | grep 8080
   # or
   ss -ln | grep 8080
   ```

2. **Test Local Access**:

   ```bash
   # Test from same machine
   curl http://localhost:8080/

   # Should return WebDAV response
   ```

3. **Check Network Access**:
   ```bash
   # Test from remote machine
   curl http://altmount-server-ip:8080/
   ```

**Solutions:**

1. **Firewall Configuration**:

   ```bash
   # Ubuntu/Debian
   sudo ufw allow 8080

   # CentOS/RHEL
   sudo firewall-cmd --add-port=8080/tcp --permanent
   sudo firewall-cmd --reload

   # Check iptables
   sudo iptables -L | grep 8080
   ```

2. **Network Interface Binding**:

   ```yaml
   # If running in Docker, ensure proper port mapping
   ports:
     - "8080:8080"
   # For CLI, AltMount binds to all interfaces by default
   ```

3. **Router/NAT Configuration**:
   ```bash
   # For external access, configure port forwarding
   # Router settings: External Port 8080 -> Internal IP:8080
   ```

_[Screenshot placeholder: Network diagnostic tools showing successful connections to AltMount WebDAV server]_

## Authentication Issues

### WebDAV Authentication Failures

#### Symptoms

```
401 Unauthorized
```

or

```
HTTP Basic: Access denied
```

**Solutions:**

1. **Verify Credentials**:

   ```yaml
   webdav:
     user: "correct_username" # Check case sensitivity
     password: "correct_password" # Check special characters
   ```

2. **Test Without Authentication**:

   ```yaml
   webdav:
     port: 8080
     # Remove user and password temporarily
   ```

3. **URL Encoding Issues**:

   ```bash
   # If password has special characters, URL encode them
   # @ becomes %40, & becomes %26, etc.
   curl http://user:p%40ssw0rd@localhost:8080/
   ```

4. **Client-Specific Issues**:

   ```bash
   # Windows WebDAV client issues
   # May need registry changes for HTTP (vs HTTPS)

   # Try alternative clients like WinSCP or Cyberduck
   ```

_[Screenshot placeholder: WebDAV client successfully connecting after authentication fix]_

## Performance Issues

### Slow Download Speeds

#### Symptoms

- Downloads significantly slower than expected
- Streaming buffers frequently
- High latency in file operations

**Diagnosis:**

1. **Check Provider Performance**:

   ```bash
   # Check provider connection utilization
   curl -u user:pass http://localhost:8080/api/providers
   ```

2. **Monitor System Resources**:

   ```bash
   # Check CPU and memory usage
   top
   htop

   # Check disk I/O
   iotop
   iostat -x 1
   ```

3. **Network Analysis**:

   ```bash
   # Check bandwidth usage
   iftop
   nethogs

   # Test provider speed directly
   wget --user=username --password=password \
        ftp://provider.com/test-file.bin
   ```

**Solutions:**

1. **Optimize Streaming Settings**:

   ```yaml
   streaming:
     max_prefetch: 45 # Increase prefetch for better throughput
   ```

2. **Provider Optimization**:

   ```yaml
   providers:
     - host: "fastest-endpoint.com" # Use fastest endpoint
       max_connections: 30 # Increase if provider allows
       tls: true
   ```

3. **System Optimization**:

   ```bash
   # Use faster storage for metadata
   # Move metadata to SSD if using HDD

   # Increase network buffers (Linux)
   echo 'net.core.rmem_max = 16777216' >> /etc/sysctl.conf
   echo 'net.core.wmem_max = 16777216' >> /etc/sysctl.conf
   ```

_[Screenshot placeholder: Performance monitoring dashboard showing improved speeds after optimization]_

### High Memory Usage

#### Symptoms

- AltMount using excessive memory
- System becomes unresponsive
- Out of memory errors

**Solutions:**

1. **Reduce Memory Settings**:

   ```yaml
   streaming:
     max_prefetch: 10 # Lower prefetch to reduce memory usage
   ```

2. **Monitor Memory Leaks**:

   ```bash
   # Monitor AltMount memory usage over time
   watch -n 5 'ps aux | grep altmount'

   # Check for gradual increases indicating leaks
   ```

3. **System Configuration**:
   ```bash
   # Increase swap space if needed
   sudo fallocate -l 2G /swapfile
   sudo chmod 600 /swapfile
   sudo mkswap /swapfile
   sudo swapon /swapfile
   ```

## File Access Issues

### Files Appear Empty or Corrupted

#### Symptoms

- Files show correct size but won't open
- Media files won't play
- Partial file downloads

**Solutions:**

1. **Check Provider Availability**:

   ```bash
   # Verify providers are connected and working
   curl -u user:pass http://localhost:8080/api/health/providers
   ```

2. **Test Different Files**:

   ```bash
   # Try accessing different files to isolate the issue
   # Check if problem is specific to certain content
   ```

3. **Clear Metadata Cache**:

   ```bash
   # Stop AltMount
   ./altmount stop

   # Clear metadata cache (backup first!)
   cp -r metadata metadata.backup
   rm -rf metadata/*

   # Restart AltMount
   ./altmount serve
   ```

4. **Enable Auto-Repair**:

   ```yaml
   health:
     enabled: true

   arrs:
     enabled: true
     # Configure ARR instances — auto-repair activates automatically
     # when ARR integration is enabled and configured
   ```

_[Screenshot placeholder: File verification process showing corrupt file detection and repair initiation]_

### Permission Denied on Files

#### Symptoms

```
Permission denied
```

or

```
403 Forbidden
```

**Solutions:**

1. **Check File Permissions**:

   ```bash
   # Check metadata directory permissions
   ls -la metadata/

   # Fix ownership if needed
   sudo chown -R altmount:altmount metadata/
   ```

2. **Docker Permission Issues**:

   ```yaml
   # Ensure PUID/PGID match host user
   environment:
     - PUID=1000
     - PGID=1000

   # Fix volume permissions
   sudo chown -R 1000:1000 ./config ./metadata
   ```

3. **WebDAV Authentication**:
   ```bash
   # Verify WebDAV credentials are correct
   curl -u username:password http://localhost:8080/path/to/file
   ```

## Database Issues

### Database Locked Errors

#### Symptoms

```
database is locked
```

or

```
SQLITE_BUSY: database is locked
```

**Solutions:**

1. **Check for Multiple Instances**:

   ```bash
   # Ensure only one AltMount instance is running
   ps aux | grep altmount

   # Kill any extra processes
   sudo pkill altmount
   ```

2. **Database Recovery**:

   ```bash
   # Stop AltMount
   ./altmount stop

   # Check database integrity
   sqlite3 altmount.db "PRAGMA integrity_check;"

   # Repair if needed
   sqlite3 altmount.db ".backup altmount.db.backup"
   ```

3. **File System Issues**:

   ```bash
   # Check if database is on network storage
   # Move to local storage if needed
   mv altmount.db /local/storage/altmount.db

   # Update config to point to new location
   ```

_[Screenshot placeholder: Database diagnostic commands showing successful integrity check and repair]_

### Database Read-Only Errors

#### Symptoms

```
Error: failed to start NZB service: failed to reset stale queue items: failed to reset stale queue items: attempt to write a readonly database
```

or

```
SQLITE_READONLY: attempt to write a readonly database
```

**Solutions:**

1. **Fix Database Permissions**:

   ```bash
   # Make database writable
   chmod +w altmount.db

   # Verify permissions
   ls -la altmount.db
   # Should show: -rw-rw-rw- or similar with write permissions
   ```

2. **Check Directory Permissions**:

   ```bash
   # Ensure the directory containing the database is writable
   chmod +w /path/to/altmount/directory

   # Check directory permissions
   ls -ld /path/to/altmount/directory
   ```

3. **Docker Permission Issues**:

   ```bash
   # If running in Docker, fix ownership
   sudo chown -R 1000:1000 ./altmount.db

   # Or ensure proper PUID/PGID in docker-compose.yml
   environment:
     - PUID=1000
     - PGID=1000
   ```

4. **File System Mount Issues**:

   ```bash
   # Check if database is on a read-only filesystem
   mount | grep $(dirname $(realpath altmount.db))

   # If mounted read-only, remount as read-write
   sudo mount -o remount,rw /path/to/mount/point
   ```

_[Screenshot placeholder: Terminal showing database permission fix and successful AltMount startup]_

## Import Issues

### Imports Stuck in Processing State (Purple)

#### Symptoms

- Imports show a purple/processing status indefinitely
- Queue items never complete or fail
- System may become sluggish

**Causes:**

1. **ffprobe hanging**: Media analysis can hang on certain files, blocking the import pipeline
2. **Memory exhaustion**: Large files or many concurrent imports can exhaust system memory
3. **Container issues**: Docker containers may enter a degraded state

**Solutions:**

1. **Check for stuck ffprobe processes**:

   ```bash
   # Inside the container, check for long-running ffprobe
   docker exec <container> ps -eo pid,etime,stat,cmd | grep ffprobe

   # Kill stuck processes if found
   docker exec <container> kill <pid>
   ```

2. **Restart the container**:

   ```bash
   docker restart altmount
   ```

3. **Check system memory**:

   ```bash
   # If the system ran out of memory, imports may have been killed
   free -h
   dmesg | grep -i "out of memory"
   ```

4. **Delete and re-import**: If restarting doesn't resolve the issue, delete the stuck items from the queue via the web UI and re-send them from your ARR application.

5. **Reduce concurrent workers** to prevent future occurrences:

   ```yaml
   import:
     max_processor_workers: 1 # Reduce from default
   ```

### Symlink Paths Not Working with Docker

#### Symptoms

- Symlink directories disappear after container restart
- ARR apps can't find imported files
- Imports succeed but ARR shows "file not found"

**Solutions:**

1. **Ensure consistent volume mappings**: Both AltMount and ARR containers must see the same paths. The symlink target must resolve inside both containers:

   ```yaml
   services:
     altmount:
       volumes:
         - /data/imports:/data/imports
         - /data/media:/data/media:rshared  # rshared for FUSE mounts

     sonarr:
       volumes:
         - /data/imports:/data/imports
         - /data/media:/data/media
   ```

2. **Why symlinks disappear after restart**: Symlinks are created inside the container filesystem. If the `import_dir` is not mounted as a persistent volume, symlinks are lost on restart. Always mount `import_dir` as a Docker volume.

3. **Verify path alignment**:

   ```bash
   # Inside AltMount container, check symlink targets
   docker exec altmount ls -la /data/imports/

   # Inside ARR container, verify the target exists
   docker exec sonarr ls -la /data/media/
   ```

4. **Check `mount_path` configuration**: The `mount_path` in your config must match the path where the WebDAV/FUSE mount is visible inside the ARR container, not the host path.

## Provider Issues

### Providers Showing Offline

#### Symptoms

- Provider status shows as offline or disconnected
- Downloads fail with connection errors
- Health checks report all segments as missing

**Solutions:**

1. **Pull the latest AltMount image**:

   ```bash
   docker pull javi11/altmount:latest
   docker restart altmount
   ```

2. **Verify provider configuration**:

   ```bash
   # Test provider connectivity
   telnet <provider-host> <port>
   ```

3. **Check provider credentials**: Some providers rotate hostnames or require password resets. Verify your credentials on the provider's website.

4. **Check connection limits**: If you're running AltMount alongside other Usenet clients, you may be exceeding the provider's maximum connection limit. Reduce `max_connections` in your config.

5. **Try alternative endpoints**: Many providers offer multiple server hostnames. Switch to a different endpoint if your current one is unreachable.

## Provider Pipelining Issues

### Symptoms

- Random connection drops or timeouts during downloads
- Corrupted or incomplete segment downloads
- `480` or unexpected error responses from the provider
- Downloads work initially but fail mid-stream
- Errors like `unexpected response code` or `connection reset by peer`

**Cause:**

NNTP pipelining sends multiple requests on a single connection before waiting for responses (controlled by the `inflight_requests` setting). While this improves throughput, some providers or network configurations don't handle aggressive pipelining well — they may drop connections, return errors, or corrupt responses when too many requests are in-flight simultaneously.

**Solutions:**

1. **Disable pipelining** by setting `inflight_requests` to `1`:

   ```yaml
   providers:
     - host: "ssl-news.provider.com"
       port: 563
       username: "your_username"
       password: "your_password"
       max_connections: 20
       tls: true
       inflight_requests: 1  # Disable pipelining — one request at a time
   ```

   This is the safest and most compatible setting. It forces sequential request-response behavior on each connection, eliminating any pipelining-related issues.

2. **Gradually increase** once stable. If `inflight_requests: 1` resolves the issue, you can try increasing it gradually (e.g., `2`, `5`, `10`) to find the highest value your provider supports reliably.

3. **Compensate with more connections**. With lower `inflight_requests`, you can increase `max_connections` to maintain overall throughput. For example, switching from 20 connections with 10 inflight to 30 connections with 1 inflight may give comparable speeds with better stability.

4. **Check provider documentation**. Some providers document their pipelining support or recommended settings. Block account providers and smaller providers are more likely to have limited pipelining support.

:::tip
When reporting connection issues, always mention your `inflight_requests` value. Setting it to `1` is the first thing to try when debugging any provider-related download problem.
:::

## Logging and Debugging

### Enable Debug Logging

For detailed troubleshooting, enable debug logging:

```yaml
log:
  level: "debug"
  file: "/var/log/altmount/debug.log"
  max_size: 100
  max_backups: 5
```

### Useful Debug Commands

```bash
# Real-time log monitoring
tail -f /var/log/altmount/altmount.log

# Search for specific errors
grep -i "error" /var/log/altmount/altmount.log

# Check recent entries
journalctl -u altmount -f --since "1 hour ago"

# API health check
curl -u user:pass http://localhost:8080/api/health/detailed | jq .

# Test WebDAV directly
curl -I -u user:pass http://localhost:8080/

# Provider connection test
curl -u user:pass http://localhost:8080/api/providers/provider-name/test
```

_[Screenshot placeholder: Terminal showing debug log output with detailed request/response information]_

## Getting Help

### Information to Gather

Before seeking help, gather this information:

1. **System Information**:

   ```bash
   # AltMount version
   ./altmount --version

   # System information
   uname -a

   # Available resources
   free -h
   df -h
   ```

2. **Configuration** (remove sensitive information):

   ```bash
   # Sanitized config
   cat config.yaml | sed 's/password: .*/password: [REDACTED]/'
   ```

3. **Recent Logs**:

   ```bash
   # Last 100 lines with timestamps
   tail -n 100 /var/log/altmount/altmount.log
   ```

4. **Health Status**:
   ```bash
   # Current system health
   curl -u user:pass http://localhost:8080/api/health/detailed
   ```

### Support Channels

- **GitHub Issues**: [https://github.com/javi11/altmount/issues](https://github.com/javi11/altmount/issues)
- **GitHub Discussions**: [https://github.com/javi11/altmount/discussions](https://github.com/javi11/altmount/discussions)
- **Documentation**: This documentation site

### Issue Reporting Template

````markdown
## Problem Description

Brief description of the issue

## Environment

- AltMount Version:
- OS:
- Installation Method: (CLI/Docker)

## Configuration

```yaml
# Paste relevant config (remove passwords)
```
````

## Steps to Reproduce

1.
2.
3.

## Expected Behavior

What should happen

## Actual Behavior

What actually happens

## Logs

```
# Paste relevant log entries
```

## Additional Context

Any other relevant information

````

*[Screenshot placeholder: GitHub issue template showing proper information organization for support requests]*

## Prevention Best Practices

### Regular Maintenance

1. **Monitor Health Status**:
   ```bash
   # Daily health check script
   #!/bin/bash
   curl -s -u user:pass http://localhost:8080/api/health | jq .
````

2. **Log Rotation**:

   ```yaml
   log:
     max_size: 100 # Rotate at 100MB
     max_age: 30 # Keep for 30 days
     max_backups: 10 # Keep 10 backups
     compress: true # Compress old logs
   ```

3. **Backup Configuration**:
   ```bash
   # Regular config backup
   cp config.yaml config.yaml.backup.$(date +%Y%m%d)
   ```

### Performance Monitoring

1. **Set Up Monitoring**:

   ```bash
   # Monitor key metrics
   watch -n 30 'curl -s -u user:pass http://localhost:8080/api/queue/stats'
   ```

2. **Capacity Planning**:

   ```bash
   # Monitor disk usage
   df -h | grep metadata

   # Monitor memory usage
   ps aux | grep altmount | awk '{print $4}'
   ```

### Update Strategy

1. **Test Updates**: Always test updates in a staging environment
2. **Backup First**: Backup configuration and critical data before updates
3. **Read Changelog**: Review changes and breaking updates
4. **Monitor Post-Update**: Watch logs and performance after updates

---

## Next Steps

- **[Performance Optimization](performance.md)** - Optimize AltMount performance
- **[Health Monitoring](../3. Configuration/health-monitoring.md)** - Set up comprehensive monitoring
