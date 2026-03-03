# Stremio Integration

AltMount provides a dedicated endpoint for [Stremio](https://www.stremio.com/) add-on developers and power users who want to stream Usenet content directly through Stremio. Upload an NZB file, and AltMount will queue it with high priority, wait for the download to complete (or reach a streamable state), and return Stremio-compatible stream URLs for every media file found in the output.

## Overview

1. A client POSTs an NZB file to `POST /api/nzb/streams`.
2. AltMount adds the NZB to the queue with elevated priority.
3. The request blocks (long-polls) until the content is ready or the timeout is reached.
4. AltMount returns a list of stream objects whose URLs point directly to the mounted media files, ready for Stremio to play.

Because the request blocks synchronously, the add-on can hand the result straight to `callback({ streams })` — no polling required.

## Prerequisites

- Stremio integration enabled in your configuration (`stremio.enabled: true`).
- At least one NNTP provider configured and online.
- You know your AltMount API key (visible in **Settings > API Key**).

## Configuration

Add the following block to your `config.yaml`:

```yaml
stremio:
  enabled: true
  nzb_ttl_hours: 24   # 0 = keep cached streams forever
  base_url: ""        # optional — set if auto-detection gives the wrong origin
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable the `/api/nzb/streams` endpoint |
| `nzb_ttl_hours` | int | `24` | Hours before a cached NZB result expires. `0` means never expire. |
| `base_url` | string | `""` | Public base URL used when building stream links (e.g. `https://altmount.example.com`). When empty, AltMount auto-detects the origin from the incoming request. Set this when running behind a reverse proxy or when the detected origin is wrong. |

When `nzb_ttl_hours` is greater than zero, submitting the same NZB filename within the TTL window returns the cached stream URLs immediately without re-queueing or re-downloading.

## Authentication — the `download_key`

The streams endpoint does **not** accept your raw API key. Instead it expects a `download_key`, which is the lowercase hex SHA-256 hash of your API key:

```
download_key = sha256(api_key)   # lowercase hex
```

This is safe to embed in Stremio stream URLs and share with the Stremio app: it cannot be reversed to recover your API key, and it has no other privileges in AltMount.

Compute it once and store it in your add-on configuration:

```bash
# Linux / macOS
echo -n "YOUR_API_KEY" | sha256sum | awk '{print $1}'

# macOS (if sha256sum is not available)
echo -n "YOUR_API_KEY" | shasum -a 256 | awk '{print $1}'
```

## Endpoint Reference

### `POST /api/nzb/streams`

Submit an NZB and receive stream URLs.

**Content-Type**: `multipart/form-data`

| Field | Required | Description |
|-------|----------|-------------|
| `download_key` | Yes | SHA-256 of your API key (lowercase hex) |
| `file` | Yes | The `.nzb` file to process (max 100 MB) |
| `category` | No | Download category (e.g. `movies`, `tv`) |
| `timeout` | No | Seconds to wait before returning a 408 (default: `300`) |

### Response

**200 OK** — streams are ready:

```json
{
  "streams": [
    {
      "url":   "http://192.168.1.10:8080/webdav/movies/Movie.Name.2024.mkv",
      "title": "Movie.Name.2024.mkv",
      "name":  "AltMount"
    }
  ],
  "_queue_item_id": 42,
  "_queue_status":  "completed"
}
```

| Field | Description |
|-------|-------------|
| `streams[].url` | Direct HTTP URL to the media file, playable by Stremio |
| `streams[].title` | Filename shown in Stremio |
| `streams[].name` | Source label shown in Stremio (`"AltMount"`) |
| `_queue_item_id` | Internal queue ID (useful for debugging or manual follow-up) |
| `_queue_status` | Final queue status at the time of the response |

**408 Request Timeout** — the download did not complete within the timeout:

```json
{
  "success": false,
  "error": {
    "code":    "REQUEST_TIMEOUT",
    "message": "Download did not complete within the timeout period",
    "details": "queue_item_id: 42"
  }
}
```

Use `_queue_item_id` / `queue_item_id` from the error details to check progress via the queue API or retry later.

## Caching

To avoid re-downloading the same release, AltMount caches the stream URLs keyed by the NZB filename. If a second request arrives with the same filename within the TTL window, the cached streams are returned immediately.

Set `nzb_ttl_hours: 0` to cache forever (useful if your library is stable and disk space is not a concern).

## Example

```bash
# 1. Compute your download_key
DOWNLOAD_KEY=$(echo -n "YOUR_API_KEY" | sha256sum | awk '{print $1}')

# 2. Submit the NZB and get stream URLs
curl -s -X POST "http://localhost:8080/api/nzb/streams" \
  -F "download_key=${DOWNLOAD_KEY}" \
  -F "file=@/path/to/release.nzb" \
  -F "category=movies" \
  -F "timeout=300" | jq .
```

Example output:

```json
{
  "streams": [
    {
      "url":   "http://localhost:8080/webdav/movies/My.Movie.2024.mkv",
      "title": "My.Movie.2024.mkv",
      "name":  "AltMount"
    }
  ],
  "_queue_item_id": 7,
  "_queue_status":  "completed"
}
```

## Limitations

- **Synchronous long-poll**: The request blocks for up to `timeout` seconds (default 300 s). Stremio add-ons should set an appropriate HTTP timeout on their end.
- **408 on timeout**: If the download is still in progress when the timeout fires, AltMount returns 408. You can use the returned `queue_item_id` to poll the queue API and retry the streams request once the item completes.
- **Single endpoint**: There is no separate "check status" endpoint for the streams workflow; use the standard queue endpoints (`GET /api/queue/:id`) for manual follow-up.
