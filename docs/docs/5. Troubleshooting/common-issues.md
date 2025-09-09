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
     - name: "provider-name"
       host: "correct-hostname.provider.com" # Verify hostname
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
     - name: "provider-name"
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
     max_range_size: 67108864 # 64MB - Increase for better throughput
     streaming_chunk_size: 16777216 # 16MB - Larger chunks
     max_download_workers: 25 # More concurrent workers
   ```

2. **Provider Optimization**:

   ```yaml
   providers:
     - name: "provider-name"
       max_connections: 30 # Increase if provider allows
       host: "fastest-endpoint.com" # Use fastest endpoint
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
     max_range_size: 16777216 # 16MB - Smaller ranges
     streaming_chunk_size: 4194304 # 4MB - Smaller chunks
     max_download_workers: 10 # Fewer workers
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
     auto_repair_enabled: true

   arrs:
     enabled: true
     # Configure ARR instances for repair
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

## Logging and Debugging

### Enable Debug Logging

For detailed troubleshooting, enable debug logging:

```yaml
log:
  level: "debug"
  file: "/var/log/altmount/debug.log"
  max_size: 100
  max_backups: 5

webdav:
  debug: true # Enable WebDAV request logging
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
