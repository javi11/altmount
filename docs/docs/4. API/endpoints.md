---
title: API Endpoints
description: REST API reference for AltMount — authentication, NZB queue management, configuration, and status endpoints.
keywords: [altmount, api, rest, endpoints, nzb, queue, authentication, reference]
---

# API Endpoints

AltMount provides a comprehensive REST API for programmatic integration and automation. The interactive API reference — with a built-in request explorer — is available in the sidebar under **API Reference**.

## Authentication

Almost every endpoint under `/api/*` requires a **JWT** issued by `POST /api/auth/login`. The `?apikey=...` query parameter is **not** accepted on most endpoints — it only works on the small set of compatibility routes listed below. Requests without a valid JWT receive:

```json
{ "success": false, "message": "Authentication required" }
```

### Obtain a JWT

`POST /api/auth/login` accepts a JSON body of `{ "username": "...", "password": "..." }`. On success the server sets an **HTTP-only `JWT` cookie** — the token is **not** included in the response body, so you must either keep the cookie or read the `Set-Cookie` header.

**Option A — use a cookie jar (recommended for scripts):**

```bash
# 1. Log in and store the cookie
curl -c cookies.txt -X POST 'http://altmount.local:8585/api/auth/login' \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"yourpassword"}'

# 2. Reuse the cookie on subsequent requests
curl -b cookies.txt -X POST 'http://altmount.local:8585/api/health/bulk/restart' \
  -H 'Content-Type: application/json' \
  -d '{"ids":[0]}'
```

**Option B — extract the token and send it as a Bearer header:**

```bash
TOKEN=$(curl -s -i -X POST 'http://altmount.local:8585/api/auth/login' \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"yourpassword"}' \
  | awk -F'[=;]' '/^[Ss]et-[Cc]ookie: JWT=/ {print $2}')

curl -X POST 'http://altmount.local:8585/api/health/bulk/restart' \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"ids":[0]}'
```

The JWT can be presented in any of these ways: the `JWT` cookie, an `Authorization: Bearer <token>` header, or the `X-JWT` header.

### Endpoints that accept the API key query parameter

The per-user **API key** (visible in **System → Settings**) is only honoured by routes that read it explicitly. Sending `?apikey=` to any other endpoint will return “Authentication required”. The currently supported endpoints are:

| Endpoint | Accepts |
|----------|---------|
| `/sabnzbd` — SABnzbd-compatible API | `?apikey=` or `ma_username` + `ma_password` |
| `POST /api/arrs/webhook` — Sonarr/Radarr webhook | `?apikey=` (required) |
| `POST /api/import/file` — manual NZB file import | `?apikey=` (required) |

The [duplicate lookup endpoint](./duplicate-lookup.md), `POST /api/files/duplicate-lookup`, accepts the raw API key through the `X-API-Key` header. It requires that header even when interactive login is disabled and does not accept `?apikey=` or a JWT as a substitute.

For the other protected endpoints (queue, health, files, config, providers, system, FUSE, user, etc.) use the JWT flow above.

### Stremio addon

The Stremio addon is **not** authenticated with the API key. It uses a separate `download_key` (the SHA-256 of your API key) embedded in the URL: `/stremio/:key/manifest.json` and `/stremio/:key/stream/:type/:id.json`. The exact key is shown in the AltMount UI on the Stremio configuration page.

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

## SABnzbd History

`GET /sabnzbd?mode=history&output=json&apikey=YOUR_API_KEY` returns completed and failed jobs in the SABnzbd response format (`history.slots` and `history.noofslots`).

The normal history page is ordered newest first. `start` is the offset (default `0`), and `limit` is the page size (default `100`, capped at `1000`). Use `category` to filter jobs by category.

For Sonarr/Radarr compatibility, the first page (`start=0`, including when omitted) also appends up to **1,000 retained failed jobs** that are absent from the normal page. Sonarr/Radarr poll the first page, so this keeps older failures visible during bursts of newer completed jobs. The extra jobs use the same category filter and exclude jobs marked `skip_arr_notification`; their availability follows the failed-job retention policy.

This behavior applies to **all clients** calling this endpoint, without client detection:

- The first page can contain more jobs than the requested `limit`. Extra failures are appended after the normal page, so the complete response may not be in chronological order.
- A failed job appended to the first page can appear again at its normal position on a later page. Clients traversing pages should deduplicate by `nzo_id`.
- `history.noofslots` remains the total number of unique jobs in the filtered history, rather than the number of slots returned on the first page.
- Later pages (`start>0`) retain normal pagination. Requests with `nzo_ids` use direct job lookup and do not append unrelated failures.

For example, `start=0&limit=5` can return the five newest completed jobs plus an older failed job, for six slots in total.

The frontend queue (`/api/queue`) and import history (`/api/import/history`) use separate endpoints; this first-page behavior does not change their ordering or pagination.

## Response Format

REST endpoints under `/api` return a consistent JSON envelope:

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
