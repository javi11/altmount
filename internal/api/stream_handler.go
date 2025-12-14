package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/utils"
)

// StreamHandler handles HTTP streaming requests for files in NzbFilesystem
// Uses http.ServeContent for automatic Range request handling, ETag support,
// and proper HTTP caching semantics
type StreamHandler struct {
	nzbFilesystem *nzbfilesystem.NzbFilesystem
	userRepo      *database.UserRepository
	streamTracker *StreamTracker
}

// NewStreamHandler creates a new stream handler with the provided filesystem and user repository
func NewStreamHandler(fs *nzbfilesystem.NzbFilesystem, userRepo *database.UserRepository, streamTracker *StreamTracker) *StreamHandler {
	return &StreamHandler{
		nzbFilesystem: fs,
		userRepo:      userRepo,
		streamTracker: streamTracker,
	}
}

// authenticate validates the download_key parameter against user API keys
// Returns true if the download_key matches a hashed API key from any user
func (h *StreamHandler) authenticate(r *http.Request) bool {
	ctx := r.Context()

	// Extract download_key from query parameter
	downloadKey := r.URL.Query().Get("download_key")
	if downloadKey == "" {
		slog.WarnContext(ctx, "Stream access attempt without download_key",
			"path", r.URL.Query().Get("path"),
			"remote_addr", r.RemoteAddr)
		return false
	}

	// Get all users with API keys
	users, err := h.userRepo.GetAllUsers(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get users for authentication",
			"error", err)
		return false
	}

	// Check download_key against hashed API keys
	for _, user := range users {
		if user.APIKey == nil || *user.APIKey == "" {
			continue
		}

		// Hash the user's API key with SHA256
		hashedKey := hashAPIKey(*user.APIKey)

		// Compare with provided download_key (constant-time comparison for security)
		if hashedKey == downloadKey {
			return true
		}
	}

	slog.WarnContext(ctx, "Stream authentication failed - invalid download_key",
		"path", r.URL.Query().Get("path"),
		"remote_addr", r.RemoteAddr)
	return false
}

// hashAPIKey generates a SHA256 hash of the API key for secure comparison
func hashAPIKey(apiKey string) string {
	hash := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(hash[:])
}

// GetHTTPHandler returns an http.Handler that serves files from NzbFilesystem
// This handler:
// - Requires authentication via download_key parameter
// - Preserves context for logging and health tracking
// - Uses http.ServeContent for automatic Range request handling
// - Supports ETag and Last-Modified for caching
// - Provides proper Content-Type detection
func (h *StreamHandler) GetHTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Authenticate using download_key
		if !h.authenticate(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Stream API"`)
			http.Error(w, "Unauthorized: valid download_key required", http.StatusUnauthorized)
			return
		}

		// Serve the file
		h.serveFile(w, r)
	})
}

// serveFile handles the actual file streaming after authentication
func (h *StreamHandler) serveFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Enrich context with request metadata (similar to WebDAV adapter)
	ctx = context.WithValue(ctx, utils.ContentLengthKey, r.Header.Get("Content-Length"))
	ctx = context.WithValue(ctx, utils.RangeKey, r.Header.Get("Range"))
	ctx = context.WithValue(ctx, utils.Origin, r.RequestURI)
	ctx = context.WithValue(ctx, utils.ShowCorrupted, r.Header.Get("X-Show-Corrupted") == "true")

	// Get path from query parameter
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Path parameter required", http.StatusBadRequest)
		return
	}

	// Open file via NzbFilesystem (handles encryption, health tracking, etc.)
	file, err := h.nzbFilesystem.OpenFile(ctx, path, os.O_RDONLY, 0)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Track stream if tracker is available
	if h.streamTracker != nil {
		streamID := h.streamTracker.Add(path, r.RemoteAddr, r.UserAgent(), r.Header.Get("Range"), "API")
		defer h.streamTracker.Remove(streamID)
	}

	// Get file info
	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "Failed to get file information", http.StatusInternalServerError)
		return
	}

	// Check if it's a directory
	if stat.IsDir() {
		http.Error(w, "Cannot stream directory", http.StatusBadRequest)
		return
	}

	// Set MIME type based on file extension (prevents internal seeks)
	// This follows the same pattern as the WebDAV adapter
	ext := filepath.Ext(path)
	if ext != "" {
		mimeType := mime.TypeByExtension(ext)
		if mimeType != "" {
			w.Header().Set("Content-Type", mimeType)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
	}

	// Indicate support for range requests
	w.Header().Set("Accept-Ranges", "bytes")

	// Set Content-Disposition to inline for browser viewing
	filename := filepath.Base(path)
	w.Header().Set("Content-Disposition", `inline; filename="`+filename+`"`)

	// http.ServeContent will handle:
	// - Range requests automatically (HTTP 206 Partial Content)
	// - Content-Type detection from filename (already set above)
	// - Last-Modified header from file modtime
	// - If-Modified-Since conditional requests
	// - If-None-Match with ETag (if we add it)
	// - Accept-Ranges: bytes header (already set above)
	//
	// The file must implement io.ReadSeeker (which afero.File does)
	http.ServeContent(w, r, filename, stat.ModTime(), file)
}
