---
title: API Endpoints
description: REST API reference for AltMount — authentication, NZB queue management, configuration, and status endpoints.
keywords: [altmount, api, rest, endpoints, nzb, queue, authentication, reference]
---

# API Endpoints

AltMount provides a comprehensive REST API for programmatic integration and automation. The interactive API reference — with a built-in request explorer — is available in the sidebar under **API Reference**.

## Authentication

AltMount supports two authentication methods:

- **Bearer token** (JWT): `Authorization: Bearer <token>` — obtained via `POST /api/auth/login`
- **API key** (query parameter): `?apikey=YOUR_API_KEY` — found in System → Settings

Most endpoints accept either method.

## Endpoint Categories

| Category | Base Path | Description |
|----------|-----------|-------------|
| **Queue** | `/api/queue` | NZB queue management, upload, stats, progress streaming |
| **Health** | `/api/health` | Health monitoring, library sync, corruption detection, repair |
| **Files** | `/api/files` | File metadata, active streams, NZB export |
| **Import** | `/api/import` | Manual file imports, NZBDav imports, scan operations |
| **Providers** | `/api/config/providers` | NNTP provider CRUD, speed tests, reordering |
| **ARRs** | `/api/arrs` | Sonarr/Radarr instances, webhooks, download client registration |
| **Config** | `/api/config` | Configuration get/update/patch/reload/validate |
| **System** | `/api/system` | System stats, health, pool metrics, cleanup, restart |
| **FUSE** | `/api/fuse` | FUSE mount start/stop/status |
| **Stremio** | `/api/nzb` + `/stremio` | Upload NZB and receive Stremio-compatible stream URLs |
| **Auth** | `/api/auth` | Login, registration, auth config |
| **User** | `/api/user` | Current user info, token refresh, API key management |

## Response Format

All endpoints return a consistent JSON envelope:

```json
{ "success": true, "data": { ... } }
```

Paginated list responses include a `meta` field:

```json
{
  "success": true,
  "data": [ ... ],
  "meta": { "total": 100, "limit": 50, "offset": 0, "count": 50 }
}
```

Errors follow:

```json
{
  "success": false,
  "error": { "code": "NOT_FOUND", "message": "Item not found", "details": "" }
}
```

For the full interactive reference with schemas and a try-it-out console, visit the **[API Explorer](/api-explorer)** page.
