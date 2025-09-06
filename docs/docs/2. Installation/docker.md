# Docker Installation

This guide covers running AltMount using Docker, which provides the easiest and most consistent deployment experience across different platforms.

## Prerequisites

- Docker installed (version 20.10+ recommended)
- Docker Compose (optional but recommended)
- 1GB+ available disk space for the container and data
- Network access to Docker Hub and your Usenet providers

## Quick Start

### Using Docker Run

The fastest way to get AltMount running:

```bash
# Create directories for persistent data
mkdir -p ./config ./metadata

# Run AltMount
docker run -d \
  --name altmount \
  -p 8080:8080 \
  -v $(pwd)/config:/config \
  -v $(pwd)/metadata:/metadata \
  -e PUID=1000 \
  -e PGID=1000 \
  ghcr.io/javi11/altmount:latest
```

_[Screenshot placeholder: Terminal output showing Docker container starting successfully]_

### Using Docker Compose (Recommended)

Create a `docker-compose.yml` file:

```yaml
services:
  altmount:
    image: ghcr.io/javi11/altmount:latest
    container_name: altmount
    environment:
      - PUID=1000
      - PGID=1000
      - PORT=8080
      - HOST=0.0.0.0
    volumes:
      - ./config:/config
      - ./metadata:/metadata
    ports:
      - "8080:8080"
    restart: unless-stopped
    healthcheck:
      test:
        [
          "CMD",
          "wget",
          "--no-verbose",
          "--tries=1",
          "--spider",
          "http://localhost:8080/live",
        ]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s
```

Then run:

```bash
# Create directories
mkdir -p ./config ./metadata

# Start the service
docker-compose up -d

# Check logs
docker-compose logs -f altmount
```

_[Screenshot placeholder: Docker Compose output showing service startup and health check passing]_

## Configuration

### Environment Variables

AltMount supports several environment variables for Docker deployments:

| Variable | Description                    | Default   |
| -------- | ------------------------------ | --------- |
| `PUID`   | User ID for file permissions   | `1000`    |
| `PGID`   | Group ID for file permissions  | `1000`    |
| `PORT`   | Port to bind the web interface | `8080`    |
| `HOST`   | Host interface to bind         | `0.0.0.0` |

### Volumes

Mount these directories for persistent data:

| Container Path | Purpose             | Required |
| -------------- | ------------------- | -------- |
| `/config`      | Configuration files | Yes      |
| `/metadata`    | Metadata storage    | Yes      |

## Next Steps

- [Configure NNTP Providers](../configuration/providers.md)
- [Set up Radarr/Sonarr Integration](../configuration/integration.md)
- [Configure WebDAV Clients](../usage/webdav-clients.md)
- [Monitor with Health Checks](../usage/health-monitoring.md)
