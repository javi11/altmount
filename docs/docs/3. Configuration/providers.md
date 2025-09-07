# NNTP Providers Configuration

NNTP providers are the backbone of AltMount - they provide access to Usenet newsgroups for downloading content. This guide covers configuring multiple providers for optimal performance and reliability.

## Overview

AltMount supports multiple NNTP providers with automatic failover and load balancing. You can add as many providers as you want for maximum reliability and performance. Having multiple providers ensures:

- **Redundancy**: If one provider is down, others continue working
- **Load Distribution**: Downloads spread across providers for better performance
- **Missing Article Recovery**: When articles are missing, all other providers are automatically checked
- **Backup Provider Strategy**: Designated backup providers only used when primary providers fail
- **Content Completion**: Different providers may have different retention and content availability

## Basic Provider Configuration

### Provider Configuration via Web Interface

Configure NNTP providers through the AltMount web interface:

![Providers Overview](../../static/img/providers.png)
_AltMount web interface showing configured providers with status indicators_

### Adding a New Provider

To add a new NNTP provider, click the "Add Provider" button:

![Add Provider Dialog](../../static/img/add_provider.png)
_Provider configuration dialog showing all available settings_

**Required Fields:**

- **Host**: Your provider's server hostname
- **Port**: Usually 563 (SSL) or 119 (unencrypted)
- **Username**: Your account username
- **Password**: Your account password
- **Max Connections**: Number of concurrent connections (typically 10-50)

**Security Options:**

- **Use TLS/SSL encryption**: Recommended for security
- **Skip TLS certificate verification**: Only use if provider has certificate issues
- **Use only as backup provider**: Configure as backup/fallback provider (critical feature)

**Backup Provider Strategy:**

The "Use only as backup provider" option is crucial for building resilient provider configurations:

- **Primary Providers**: Fast, unlimited providers used for regular downloads
- **Backup Providers**: Only activated when primary providers fail or missing articles
- **Automatic Failover**: Missing articles trigger automatic backup provider usage
- **Cost Optimization**: Use expensive/limited providers only when needed

## Configuration Parameters

### Required Parameters

| Parameter         | Description                         | Example                      |
| ----------------- | ----------------------------------- | ---------------------------- |
| `name`            | Unique identifier for this provider | `"primary-ssl"`              |
| `host`            | NNTP server hostname                | `"ssl-news.provider.com"`    |
| `port`            | NNTP server port                    | `563` (SSL) or `119` (plain) |
| `username`        | Your account username               | `"your_username"`            |
| `password`        | Your account password               | `"your_password"`            |
| `max_connections` | Maximum concurrent connections      | `20`                         |

### Optional Parameters

| Parameter      | Description                       | Default | Notes                    |
| -------------- | --------------------------------- | ------- | ------------------------ |
| `tls`          | Enable SSL/TLS encryption         | `false` | Recommended for security |
| `insecure_tls` | Skip TLS certificate verification | `false` | Only for debugging       |

## Connection Types

### SSL/TLS Connections (Recommended)

![Security Options](../../static/img/add_provider.png)
_Security options in the provider configuration dialog_

**Security Recommendations:**

1. **Use TLS/SSL encryption**: ✅ Always enable for security

   - Encrypts all data between AltMount and your provider
   - Protects your credentials and download activity
   - Standard on port 563

2. **Skip TLS certificate verification**: ⚠️ Use with caution
   - Only enable if provider has certificate issues
   - Creates security risk by allowing man-in-the-middle attacks
   - Try to resolve certificate issues with provider first

**Benefits of SSL/TLS:**

- Encrypted connection protects your credentials
- Prevents ISP throttling and monitoring
- Required by most modern providers

## Performance Optimization

### Connection Limits

Set the "Max Connections" field based on your provider's limits and your bandwidth:

![Connection Settings](../../static/img/add_provider.png)
_Max Connections setting in the provider configuration_

**Guidelines**:

- **Total across all providers**: Don't exceed your bandwidth capacity

### Provider Priority and Backup Strategy

AltMount automatically manages provider selection with intelligent failover:

**Primary Provider Selection:**

- **Provider order** = First configured providers are preferred
- **Connection availability** = Providers with available connections are preferred

**Backup Provider Usage:**

- **Backup providers** = Only used when primary providers fail or lack articles
- **Missing article recovery** = All providers (including backups) checked for missing content
- **Automatic failover** = Seamless switching when primary providers unavailable

**Strategic Configuration:**

- **Primary (unlimited)**: 20-50 connections, backup=false
- **Secondary (unlimited, different backbone)**: 15-30 connections, backup=false
- **Backup (block/limited)**: 5-15 connections, backup=true
- **Specialty (European/retention)**: 10-20 connections, backup=true (for specific content)

## Testing and Validation

### Connection Testing

After configuring providers, test the connections:

1. **Start AltMount** with your configuration
2. **Check the logs** for connection status
3. **Use the web interface** to verify provider status

![Provider Status Dashboard](../../static/img/providers.png)
_Provider status dashboard showing connection counts and health indicators_

### Troubleshooting Connection Issues

#### Authentication Failures

```
ERROR Provider "primary": authentication failed
```

**Solutions**:

- Verify username and password are correct
- Check if your account is active and not suspended
- Ensure you're not exceeding connection limits

#### SSL/TLS Issues

```
ERROR Provider "primary": TLS handshake failed
```

**Solutions**:

- Verify the provider supports SSL on the specified port
- Try with `insecure_tls: true` temporarily for debugging
- Check if firewall is blocking the connection

#### Connection Limits

```
WARN Provider "primary": max connections reached (480 errors)
```

**Solutions**:

- Reduce `max_connections` for that provider
- Check provider's connection limits
- Ensure you're not running multiple clients with the same account

#### DNS/Network Issues

```
ERROR Provider "primary": failed to resolve host
```

**Solutions**:

- Verify the hostname is correct
- Test DNS resolution: `nslookup ssl-news.provider.com`
- Check network connectivity and firewall settings

## Next Steps

With providers configured:

1. **[Set up ARR Integration](integration.md)** - Connect your media applications
2. **[Configure Streaming](streaming.md)** - Optimize download performance

---
