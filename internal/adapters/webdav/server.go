package webdav

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	_ "net/http/pprof"

	"github.com/go-pkgz/auth/v2/token"
	"github.com/javi11/altmount/internal/adapters/webdav/propfind"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/utils"
	"github.com/spf13/afero"
	"golang.org/x/net/webdav"
)

type webdavServer struct {
	srv         *http.Server
	authCreds   *AuthCredentials
}

func NewServer(
	config *Config,
	fs afero.Fs,
	mux *http.ServeMux, // Use shared mux instead
	tokenService *token.Service, // Optional token service for JWT auth
	userRepo *database.UserRepository, // Optional user repository for JWT auth
) (*webdavServer, error) {
	// Create dynamic auth credentials
	authCreds := NewAuthCredentials(config.User, config.Pass)
	// Create custom error handler that maps our errors to proper HTTP status codes
	errorHandler := &customErrorHandler{
		fileSystem: aferoToWebdavFS(fs),
	}

	handler := &webdav.Handler{
		FileSystem: errorHandler,
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil && !errors.Is(err, context.Canceled) {
				slog.DebugContext(r.Context(), "WebDav error", "err", err)
			}
		},
	}

	// Add pprof endpoints for profiling only in debug mode
	if config.Debug {
		mux.HandleFunc("/debug/pprof/", http.DefaultServeMux.ServeHTTP)
		mux.HandleFunc("/debug/pprof/profile", http.DefaultServeMux.ServeHTTP)
		mux.HandleFunc("/debug/pprof/symbol", http.DefaultServeMux.ServeHTTP)
		mux.HandleFunc("/debug/pprof/trace", http.DefaultServeMux.ServeHTTP)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Fallback to basic authentication if JWT failed
		username, password, hasBasicAuth := r.BasicAuth()

		var authenticated bool
		if !hasBasicAuth {
			// Try JWT token authentication first (if services are available)
			if tokenService != nil && userRepo != nil {
				claims, _, err := tokenService.Get(r)
				if err == nil && claims.User != nil {
					// Valid token found, check user exists in database
					userID := claims.User.ID
					if userID == "" {
						userID = claims.Subject
					}

					if userID != "" {
						user, err := userRepo.GetUserByID(userID)
						if err == nil && user != nil {
							authenticated = true
						}
					}
				}
			}
		} else {
			// Check against dynamic credentials
			currentUser, currentPass := authCreds.GetCredentials()
			if username == currentUser && password == currentPass {
				authenticated = true
			}
		}

		if !authenticated {
			w.Header().Set("WWW-Authenticate", `Basic realm="BASIC WebDAV REALM"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, err := w.Write([]byte("401 Unauthorized"))
			if err != nil {
				slog.ErrorContext(r.Context(), "Error writing the response to the client", "err", err)
			}
			return
		}

		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/webdav")

		// This will prevent webdav internal seeks which is not supported by usenet reader
		ext := filepath.Ext(r.URL.Path)
		if ext != "" {
			mimeType := mime.TypeByExtension(ext)
			if mimeType != "" {
				w.Header().Set("Content-Type", mimeType)
			} else {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
		}

		w.Header().Set("Accept-Ranges", "bytes")
		r = r.WithContext(context.WithValue(r.Context(), utils.ContentLengthKey, r.Header.Get("Content-Length")))
		r = r.WithContext(context.WithValue(r.Context(), utils.RangeKey, r.Header.Get("Range")))
		r = r.WithContext(context.WithValue(r.Context(), utils.IsCopy, r.Method == "COPY"))
		r = r.WithContext(context.WithValue(r.Context(), utils.Origin, r.RequestURI))

		if r.Method == "PROPFIND" {
			status, err := propfind.HandlePropfind(handler.FileSystem, handler.LockSystem, w, r)
			if status != 0 {
				w.WriteHeader(status)
				if status != http.StatusNoContent {
					_, _ = w.Write([]byte(webdav.StatusText(status)))
					return
				}
			}

			if err != nil {
				slog.ErrorContext(r.Context(), "Error handling the request", "err", err)
				return
			}

			return
		}

		handler.ServeHTTP(w, r)
	})
	addr := fmt.Sprintf(":%v", config.Port)

	srv := &http.Server{
		Addr: addr,
		// Good practice to set timeouts to avoid Slowloris attacks.
		IdleTimeout:  time.Minute * 5,
		WriteTimeout: time.Minute * 30,
		Handler:      mux,
	}

	return &webdavServer{
		srv:       srv,
		authCreds: authCreds,
	}, nil
}

func (s *webdavServer) Start(ctx context.Context) error {
	slog.InfoContext(ctx, fmt.Sprintf("WebDav server started at %s/webdav", s.srv.Addr))

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		slog.InfoContext(ctx, "WebDav server received shutdown signal")
		// Shutdown server gracefully
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			slog.ErrorContext(ctx, "Error during WebDav server shutdown", "err", err)
			return err
		}
		slog.InfoContext(ctx, "WebDav server stopped gracefully")
		return nil
	case err := <-serverErr:
		if err != nil {
			slog.ErrorContext(ctx, "Failed to start WebDav server", "err", err)
			return err
		}
		return nil
	}
}

func (s *webdavServer) Stop() {
	slog.Info("Stopping WebDav server")

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	err := s.srv.Shutdown(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to shutdown WebDav server", "err", err)
	}

	slog.Info("WebDav server stopped")
}

// GetAuthCredentials returns the auth credentials for dynamic updates
func (s *webdavServer) GetAuthCredentials() *AuthCredentials {
	return s.authCreds
}
