# Rclone Docker Volume Plugin

The Docker Rclone Plugin enables mounting remote storage like WebDAV directly as a Docker volume, allowing containers to transparently access external filesystems as if they were local volumes.

This page explains how to install and configure the rclone Docker Volume Plugin.

See rclone [docs](https://rclone.org/docker/) for more info.

> **Note:**  
> The example compose uses a volume with the `rclone` driver.  
> Make sure the plugin is installed on the Docker host before running `docker compose up`.

---

## Installation command

Verify that the FUSE 3 driver is installed on your system:

```bash
fusermount3 --version
```

Install the FUSE driver if needed:

```bash
sudo apt install fuse3
```

> **Note:**  
> The rclone Docker Volume Plugin requires **FUSE 3**.  
> If your system uses FUSE 2, upgrade it before proceeding.

Run the following commands on your Docker host to install the rclone Docker Volume Plugin:

```bash
sudo mkdir -p /var/lib/docker-plugins/rclone/config
sudo mkdir -p /var/lib/docker-plugins/rclone/cache

docker plugin install rclone/docker-volume-rclone:amd64 \
  args="-v --links --uid=1000 --gid=1000 --async-read=true --allow-non-empty --allow-other \
  --rc --rc-no-auth --rc-addr=0.0.0.0:5572 --vfs-read-ahead=128M --vfs-read-chunk-size=32M \
  --vfs-read-chunk-size-limit=2G --vfs-cache-mode=full --vfs-cache-max-age=504h \
  --vfs-cache-max-size=50G --buffer-size=32M --dir-cache-time=10m --timeout=10m" \
  --alias rclone --grant-all-permissions
```

Create `rclone.conf` in `/var/lib/docker-plugins/rclone/config/`:

```ini
[altmount]
type = webdav
url = <your_endpoint>
vendor = other
user = <your_webdav_username>
pass = <your_rclone_obscured_password>  # See https://rclone.org/commands/rclone_obscure
```

---

## Docker mount

To access the volume within the Compose file, simply mount it as shown in the example below.  
Please note that the example is not a complete configuration.  
The Ubuntu container is only used to test the volume and is not required otherwise, so it can be removed.

```yaml
services:
  altmount:
    image: ...

  sonarr:
    image: ...
    volumes:
      - altmount:/mnt/altmount

  ubuntu:
    image: ubuntu
    command: sleep infinity
    volumes:
      - altmount:/mnt/altmount
    environment:
      - PUID=1000
      - PGID=1000

volumes:
  altmount:
    driver: rclone
    ...
```

---

## Mounting the volume from another stack

If you want to mount the volume from another stack, the following needs to be considered.

Find the volume name that creates the rclone mount:

```bash
docker volume ls
```

Example output:

```
DRIVER              VOLUME NAME
rclone:latest       arr-stack_altmount
```

Reference that volume in the second stack as an external volume:

```yaml
services:
  ubuntu:
    image: ubuntu
    command: sleep infinity
    volumes:
      - altmount:/mnt/altmount
    environment:
      - PUID=1000 # Must match UID value from the volume in the stack creating the volume (driver_opts)
      - PGID=1000 # Must match GID value from the volume in the stack creating the volume (driver_opts)

volumes:
  altmount:
    name: arr-stack_altmount
    external: true
```