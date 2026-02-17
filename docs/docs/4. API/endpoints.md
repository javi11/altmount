# API Endpoints

AltMount provides REST API endpoints for programmatic integration and automation.

## Authentication

All API endpoints require authentication using an API key. The API key can be found in your AltMount system settings.

API keys are provided via query parameter:

```
?apikey=YOUR_API_KEY
```

## API Endpoint Categories

AltMount exposes the following endpoint groups under the `/api` prefix:

| Category | Base Path | Description |
|----------|-----------|-------------|
| **Import** | `/api/import` | Manual file imports, NZBDav imports, scan operations |
| **Queue** | `/api/queue` | NZB queue management, upload, stats, progress streaming |
| **Health** | `/api/health` | Health monitoring, library sync, corruption detection, repair |
| **Files** | `/api/files` | File metadata, active streams, NZB export |
| **Providers** | `/api/providers` | NNTP provider CRUD, speed tests, reordering |
| **ARRs** | `/api/arrs` | Sonarr/Radarr instances, webhooks, download client registration |
| **Config** | `/api/config` | Configuration get/update/patch/reload/validate |
| **System** | `/api/system` | System stats, health, pool metrics, cleanup, restart |
| **FUSE** | `/api/fuse` | FUSE mount start/stop/status |
| **RClone** | `/api/rclone` | RClone connection test, mount management |
| **Auth** | `/api/auth` | Login, registration, auth config |
| **User** | `/api/user` | Current user info, token refresh, API key management |
| **Users** | `/api/users` | User management (admin) |

## Response Format

### Success Response

```json
{
  "success": true,
  "data": { ... }
}
```

### Success with Pagination

```json
{
  "success": true,
  "data": [ ... ],
  "meta": {
    "total": 100,
    "limit": 50,
    "offset": 0,
    "count": 50
  }
}
```

### Error Response

```json
{
  "success": false,
  "error": {
    "code": "ERROR_CODE",
    "message": "Human readable message",
    "details": "Additional context"
  }
}
```

### Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `BAD_REQUEST` | 400 | Invalid request format |
| `VALIDATION_ERROR` | 400 | Request validation failed |
| `UNAUTHORIZED` | 401 | Authentication required |
| `FORBIDDEN` | 403 | Access forbidden |
| `NOT_FOUND` | 404 | Resource not found |
| `CONFLICT` | 409 | Resource conflict |
| `INTERNAL_SERVER_ERROR` | 500 | Server error |

## Manual Import Endpoints

### Import File

**Endpoint**: `POST /api/import/file`

Manually add a file by filesystem path to the import queue. This is useful for custom integrations or scripts that need to trigger file processing.

#### Request Format

**Query Parameters**:

- `apikey` (required): Your AltMount API key

**Request Body** (JSON):

```json
{
  "file_path": "/path/to/your/file.nzb",
  "relative_path": "/path/to/strip"
}
```

**Request Body Fields**:

- `file_path` (required): Full path to the file to import
- `relative_path` (optional): Path that will be stripped from the file destination

#### Response Format

**Success Response** (200 OK):

```json
{
  "success": true,
  "data": {
    "queue_id": 123,
    "message": "File successfully added to import queue with ID 123"
  }
}
```

**Error Responses**:

- **401 Unauthorized**: Missing or invalid API key
- **400 Bad Request**: Invalid request format or missing file_path
- **404 Not Found**: File does not exist at specified path
- **409 Conflict**: File is already in the import queue
- **500 Internal Server Error**: Server error during processing

#### Example Usage

```bash
# Basic file import
curl -X POST "http://localhost:8080/api/import/file?apikey=YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"file_path": "/downloads/movie.nzb"}'

# Import with relative path
curl -X POST "http://localhost:8080/api/import/file?apikey=YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"file_path": "/downloads/subfolder/tvshow.nzb", "relative_path": "/downloads"}'
```

#### File Requirements

- File must exist and be accessible by AltMount
- File must be a regular file (not a directory)
- File must not already be in the import queue
- Supported file types: `.nzb` files and other importable formats

---
