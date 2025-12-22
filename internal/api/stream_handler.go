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
	stream *nzbfilesystem.ActiveStream
	ctx    context.Context
}

func (m *MonitoredFile) Read(p []byte) (n int, err error) {
	if err := m.ctx.Err(); err != nil {
		return 0, err
	}
	n, err = m.file.Read(p)
	if n > 0 {
		atomic.AddInt64(&m.stream.BytesSent, int64(n))
		atomic.AddInt64(&m.stream.CurrentOffset, int64(n))
	}
	return n, err
}

func (m *MonitoredFile) Seek(offset int64, whence int) (int64, error) {
	if err := m.ctx.Err(); err != nil {
		return 0, err
	}
	newOffset, err := m.file.Seek(offset, whence)
	if err == nil {
		atomic.StoreInt64(&m.stream.CurrentOffset, newOffset)
	}
	return newOffset, err
}

func (m *MonitoredFile) Close() error {
	return m.file.Close()
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

	// Authenticate again to get user details
	user, ok := h.authenticate(r)
	if !ok {
		// Should have been caught by GetHTTPHandler
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var userName string
	if user.Name != nil && *user.Name != "" {
		userName = *user.Name
	} else {
		userName = user.UserID
	}

	// Set stream source and username for tracking
	ctx = context.WithValue(ctx, utils.StreamSourceKey, "API")
	ctx = context.WithValue(ctx, utils.StreamUserNameKey, userName)
	ctx = context.WithValue(ctx, utils.ClientIPKey, r.RemoteAddr)
	ctx = context.WithValue(ctx, utils.UserAgentKey, r.UserAgent())

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

		var streamID string
		// Try to get stream ID from the file itself (created during OpenFile)
		if mvf, ok := file.(*nzbfilesystem.MetadataVirtualFile); ok {
			streamID = mvf.GetStreamID()
		}

		if streamID != "" {
			// Add stream ID to context for low-level tracking
			streamCtx = context.WithValue(streamCtx, utils.StreamIDKey, streamID)
			
			// Register cancel function in tracker
			h.streamTracker.SetCancelFunc(streamID, cancel)

			streamObj := h.streamTracker.GetStream(streamID)
			if streamObj != nil {
				// Wrap the file with monitoring
				monitoredFile := &MonitoredFile{
					file:   file,
					stream: streamObj,
					ctx:    streamCtx,
				}

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
		}
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
