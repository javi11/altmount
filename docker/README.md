# AltMount Docker Configuration

This directory contains Docker configurations for running AltMount in containerized environments using LinuxServer.io patterns.

## Files

- `Dockerfile` - Complete multi-stage build for development/local use
- `Dockerfile.ci` - Optimized CI build expecting pre-built frontend assets  
- `root/` - s6-overlay service configuration following LinuxServer.io standards

## Architecture

The containers follow the standard LinuxServer.io pattern:
- Uses `ghcr.io/linuxserver/baseimage-ubuntu:jammy` base image
- Implements proper s6-overlay service structure
- Handles user permissions through PUID/PGID environment variables
- Simple, clean service scripts without complex permission handling

## Basic Usage

```bash
docker run -d \
  --name altmount \
  -e PUID=1000 \
  -e PGID=1000 \
  -p 8080:8080 \
  -v /path/to/config:/config \
  -v /path/to/metadata:/metadata \
  your-registry/altmount:latest
```

### Environment Variables

- `PUID` - User ID for file permissions (default: 1000)
- `PGID` - Group ID for file permissions (default: 1000)
- `PORT` - Port to bind the web interface (default: 8080)
- `HOST` - Host interface to bind (default: 0.0.0.0)

### Volumes

- `/config` - Configuration files directory
- `/metadata` - Metadata storage directory

## Service Structure

The containers use s6-overlay with the following structure:

```
root/
└── etc/
    └── s6-overlay/
        └── s6-rc.d/
            ├── svc-altmount/
            │   ├── run        # Main service script
            │   └── type       # Service type (longrun)
            └── user/
                └── contents.d/
                    └── svc-altmount  # Service dependency
```

The service script is minimal and follows LinuxServer.io patterns:
```bash
#!/usr/bin/with-contenv bash
exec s6-setuidgid abc /app/altmount serve --config=/config/config.yaml
```

## Troubleshooting

### Permission Issues

If you encounter permission issues:

1. **Ensure host directory ownership** matches PUID/PGID:
   ```bash
   sudo chown -R 1000:1000 /path/to/config /path/to/metadata
   ```

2. **Check container logs** for s6-overlay initialization:
   ```bash
   docker logs altmount
   ```

3. **Verify PUID/PGID values** match your host system:
   ```bash
   id $(whoami)  # Shows your UID/GID
   ```

### Common Issues

- **Service fails to start**: Check that volumes exist and are writable
- **Permission denied on volumes**: Ensure PUID/PGID match host ownership
- **Config not loading**: Verify config file exists at `/config/config.yaml`

## Docker Compose Example

```yaml
version: '3.8'
services:
  altmount:
    image: your-registry/altmount:latest
    container_name: altmount
    environment:
      - PUID=1000
      - PGID=1000
    volumes:
      - ./config:/config
      - ./metadata:/metadata
    ports:
      - "8080:8080"
    restart: unless-stopped
```

## Building

### Development Build (includes frontend build)
```bash
docker build -f docker/Dockerfile -t altmount:dev .
```

### CI Build (expects pre-built frontend in frontend/dist)
```bash
# Build frontend first
cd frontend && bun run build && cd ..
docker build -f docker/Dockerfile.ci -t altmount:ci .
```

## Health Check

Both containers include health checks that verify the service is responding on port 8080. The health check uses `wget` to test the `/health` endpoint.

## Comparison to Previous Version

This simplified approach:
- **Reduces Dockerfile complexity** by 80%+ lines
- **Follows LinuxServer.io standards** for better compatibility  
- **Eliminates custom permission handling** in favor of base image patterns
- **Uses proper s6-overlay structure** instead of embedded scripts
- **Provides cleaner, more maintainable configuration**