# API Endpoints

AltMount provides REST API endpoints for programmatic integration and automation.

## Authentication

All API endpoints require authentication using an API key. The API key can be found in your AltMount system settings.

API keys are provided via query parameter:

```
?apikey=YOUR_API_KEY
```

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
  "data": {
    "queue_id": 123,
    "message": "File successfully added to import queue with ID 123"
  },
  "meta": null
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

## Error Handling

All API endpoints return consistent error responses:

```json
{
  "error": {
    "message": "Error description",
    "details": "Additional error context"
  }
}
```

Common HTTP status codes:

- `200`: Success
- `400`: Bad Request - Invalid input or request format
- `401`: Unauthorized - Missing or invalid API key
- `404`: Not Found - Resource does not exist
- `409`: Conflict - Resource already exists or is in use
- `500`: Internal Server Error - Server error during processing

---
