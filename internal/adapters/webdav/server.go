package webdav

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"time"

	_ "net/http/pprof"

	"github.com/javi11/altmount/internal/adapters/webdav/propfind"
	"github.com/javi11/altmount/internal/utils"
	"github.com/spf13/afero"
	"golang.org/x/net/webdav"
)

type webdavServer struct {
	srv *http.Server
	log *slog.Logger
}

func NewServer(
	config *Config,
	fs afero.Fs,
) (*webdavServer, error) {
	log := slog.Default()
	handler := &webdav.Handler{
		FileSystem: aferoToWebdavFS(fs),
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil && !errors.Is(err, context.Canceled) {
				log.DebugContext(r.Context(), "WebDav error", "err", err)
			}
		},
	}

	mux := http.NewServeMux()
	// Add pprof endpoints for profiling only in debug mode
	if config.Debug {
		mux.HandleFunc("/debug/pprof/", http.DefaultServeMux.ServeHTTP)
		mux.HandleFunc("/debug/pprof/profile", http.DefaultServeMux.ServeHTTP)
		mux.HandleFunc("/debug/pprof/symbol", http.DefaultServeMux.ServeHTTP)
		mux.HandleFunc("/debug/pprof/trace", http.DefaultServeMux.ServeHTTP)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		username, password, _ := r.BasicAuth()

		if username != config.User || password != config.Pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="BASIC WebDAV REALM"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, err := w.Write([]byte("401 Unauthorized"))
			if err != nil {
				log.ErrorContext(r.Context(), "Error writting the response to the client", "err", err)
			}
			return
		}

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
				log.ErrorContext(r.Context(), "Error handling the request", "err", err)
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
		log: log,
		srv: srv,
	}, nil
}

func (s *webdavServer) Start(ctx context.Context) error {
	s.log.InfoContext(ctx, fmt.Sprintf("WebDav server started at %s/webdav", s.srv.Addr))

	if err := s.srv.ListenAndServe(); err != nil {
		if ctx.Err() == nil {
			s.log.ErrorContext(ctx, "Failed to start WebDav server", "err", err)
		}
	}

	return nil
}

func (s *webdavServer) Stop() {
	s.log.Info("Stopping WebDav server")

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	err := s.srv.Shutdown(ctx)
	if err != nil {
		s.log.ErrorContext(ctx, "Failed to shutdown WebDav server", "err", err)
	}

	s.log.Info("WebDav server stopped")
}
