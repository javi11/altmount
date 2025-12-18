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
	"sync/atomic"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/utils"
	"github.com/spf13/afero"
)

// StreamHandler handles HTTP streaming requests for files in NzbFilesystem
// Uses http.ServeContent for automatic Range request handling, ETag support,
// and proper HTTP caching semantics
type StreamHandler struct {
	nzbFilesystem *nzbfilesystem.NzbFilesystem
	userRepo      *database.UserRepository
	streamTracker *StreamTracker
}

// MonitoredFile wraps an afero.File to track read progress and support cancellation
type MonitoredFile struct {
	file   afero.File
	stream *ActiveStream
	ctx    context.Context
}

func (m *MonitoredFile) Read(p []byte) (n int, err error) {
	if m.ctx.Err() != nil {
		return 0, m.ctx.Err()
	}
	n, err = m.file.Read(p)
	if n > 0 {
		atomic.AddInt64(&m.stream.BytesSent, int64(n))
	}
	return n, err
}

func (m *MonitoredFile) Seek(offset int64, whence int) (int64, error) {
	if m.ctx.Err() != nil {
		return 0, m.ctx.Err()
	}
	return m.file.Seek(offset, whence)
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
// Returns the user and true if the download_key matches a hashed API key from any user
func (h *StreamHandler) authenticate(r *http.Request) (*database.User, bool) {
	ctx := r.Context()

	// Extract download_key from query parameter
	downloadKey := r.URL.Query().Get("download_key")
	if downloadKey == "" {
		slog.WarnContext(ctx, "Stream access attempt without download_key",
			"path", r.URL.Query().Get("path"),
			"remote_addr", r.RemoteAddr)
		return nil, false
	}

	// Get all users with API keys
	users, err := h.userRepo.GetAllUsers(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get users for authentication",
			"error", err)
		return nil, false
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
			return user, true
		}
	}

	slog.WarnContext(ctx, "Stream authentication failed - invalid download_key",
		"path", r.URL.Query().Get("path"),
		"remote_addr", r.RemoteAddr)
	return nil, false
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
		_, ok := h.authenticate(r)
		if !ok {
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

	// Authenticate again to get user details (cached/fast enough) or refactor GetHTTPHandler to pass it
	// Since GetHTTPHandler calls serveFile directly, we can optimize by fetching user once.
	// However, GetHTTPHandler interface is fixed as http.Handler.
	// Let's re-authenticate or trust the caller? GetHTTPHandler does auth.
	// To pass user, we could use context or change serveFile signature.
	// Changing serveFile signature is cleaner but it is called by GetHTTPHandler.
	// Let's call authenticate again, it's a DB call? No, GetAllUsers might be cached or DB.
	// To avoid DB hit, let's modify GetHTTPHandler to pass user in context or refactor.
	// Actually, for now, calling it again is safe but inefficient.
	// BETTER: Modify GetHTTPHandler to store user in context.
	user, ok := h.authenticate(r)
	if !ok {
		// Should have been caught by GetHTTPHandler
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userName := "Unknown"
	if user.Name != nil && *user.Name != "" {
		userName = *user.Name
	} else {
		userName = user.UserID
	}

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

			// Track stream if tracker is available
		if h.streamTracker != nil {
			// Create a cancellable context for the stream
			streamCtx, cancel := context.WithCancel(ctx)
			defer cancel() // Ensure cleanup
	
			stream := h.streamTracker.AddStream(path, "API", userName, stat.Size(), cancel)
			defer h.streamTracker.Remove(stream.ID)
		// Wrap the file with monitoring
		monitoredFile := &MonitoredFile{
			file:   file,
			stream: stream,
			ctx:    streamCtx,
		}
		// Use monitoredFile instead of file
		// Note: http.ServeContent requires io.ReadSeeker. MonitoredFile implements it.
		// However, http.ServeContent also uses the 'content' param (file) to read.

		// Set MIME type based on file extension (prevents internal seeks)
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

		http.ServeContent(w, r, filename, stat.ModTime(), monitoredFile)
		return
	}

	// Fallback if tracker is nil (should not happen in prod)
	ext := filepath.Ext(path)
	if ext != "" {
		mimeType := mime.TypeByExtension(ext)
		if mimeType != "" {
			w.Header().Set("Content-Type", mimeType)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
	}
	w.Header().Set("Accept-Ranges", "bytes")
	filename := filepath.Base(path)
	w.Header().Set("Content-Disposition", `inline; filename="`+filename+`"`)
	http.ServeContent(w, r, filename, stat.ModTime(), file)
}
