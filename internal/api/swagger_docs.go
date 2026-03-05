// Package api provides the REST API server for AltMount.
//
// AltMount exposes a JSON REST API on the `/api` prefix.
// All responses follow a unified envelope format:
//
//	{ "success": true, "data": { ... } }
//	{ "success": false, "error": { "code": "...", "message": "...", "details": "..." } }
//
// Most endpoints require JWT Bearer authentication (set via POST /api/auth/login).
// Some endpoints also accept an API key via the `apikey` query parameter.
//
// @title           AltMount API
// @version         1.0
// @description     REST API for AltMount — a WebDAV/NFS server backed by NZB/Usenet, with queue management, file health monitoring, and media streaming.
//
// @contact.name   AltMount GitHub Issues
// @contact.url    https://github.com/javi11/altmount/issues
//
// @license.name  MIT
// @license.url   https://github.com/javi11/altmount/blob/master/LICENSE
//
// @host      localhost:8080
// @BasePath  /
//
// @securityDefinitions.apikey BearerAuth
// @in                         header
// @name                       Authorization
// @description                JWT token obtained from POST /api/auth/login. Format: "Bearer <token>"
//
// @securityDefinitions.apikey ApiKeyAuth
// @in                         query
// @name                       apikey
// @description                API key for programmatic access (available from GET /api/config)
//
// @tag.name         Queue
// @tag.description  NZB download queue — add, monitor, cancel, and retry downloads
//
// @tag.name         Health
// @tag.description  File health monitoring — track, check, and repair corrupted files
//
// @tag.name         Import
// @tag.description  Manual and automatic file import operations
//
// @tag.name         NZBDav
// @tag.description  NZBDav import endpoint for automated NZB ingestion
//
// @tag.name         Files
// @tag.description  File metadata, active stream management, and NZB export
//
// @tag.name         Config
// @tag.description  Application configuration management
//
// @tag.name         Providers
// @tag.description  NNTP provider (Usenet server) management
//
// @tag.name         System
// @tag.description  System statistics, health, browsing, and control
//
// @tag.name         Auth
// @tag.description  Authentication — login, register, and check registration status
//
// @tag.name         User
// @tag.description  User account management — profile, token refresh, API key, and logout
//
// @tag.name         FUSE
// @tag.description  FUSE/CIFS mount lifecycle management
//
// @tag.name         ARRs
// @tag.description  Sonarr/Radarr integration — instances, webhooks, and download clients
//
// @tag.name         RClone
// @tag.description  RClone remote control operations
//
// @tag.name         NZB
// @tag.description  NZB stream upload — upload an NZB and receive direct stream URLs
//
// @tag.name         Stremio
// @tag.description  Stremio addon — manifest and stream lookup (key-based auth, CORS open)
package api
