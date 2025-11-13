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

### Support Channels

- **GitHub Issues**: [https://github.com/javi11/altmount/issues](https://github.com/javi11/altmount/issues)
- **GitHub Discussions**: [https://github.com/javi11/altmount/discussions](https://github.com/javi11/altmount/discussions)
- **Documentation**: This documentation site
