package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// ServeQueueSSE is a native net/http SSE handler for GET /api/queue/stream.
// It bypasses adaptor.FiberApp which cannot stream responses (blocks on
// Response.Body() reading the SSE pipe until EOF that never comes).
func (s *Server) ServeQueueSSE(w http.ResponseWriter, r *http.Request) {
	// Replicate RequireAuth logic for net/http requests.
	loginRequired := true
	if cfg := s.configManager.GetConfig(); cfg != nil && cfg.Auth.LoginRequired != nil {
		loginRequired = *cfg.Auth.LoginRequired
	}
	if loginRequired && s.authService != nil {
		if ts := s.authService.TokenService(); ts != nil {
			if _, _, err := ts.Get(r); err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	subID, updateCh := s.progressBroadcaster.Subscribe()
	defer s.progressBroadcaster.Unsubscribe(subID)

	initialProgress := s.progressBroadcaster.GetAllProgress()
	if data, err := json.Marshal(map[string]any{"type": "initial", "data": initialProgress}); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	keepAlive := time.NewTicker(30 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case update, ok := <-updateCh:
			if !ok {
				return
			}
			if data, err := json.Marshal(map[string]any{"type": "update", "data": update}); err == nil {
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			} else {
				slog.ErrorContext(r.Context(), "failed to marshal progress update", "error", err, "queue_id", update.QueueID)
			}
		case <-keepAlive.C:
			fmt.Fprintf(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// ServeHealthSSE is a native net/http SSE handler for GET /api/health/stream.
// It bypasses adaptor.FiberApp which cannot stream responses (blocks on
// Response.Body() reading the SSE pipe until EOF that never comes).
func (s *Server) ServeHealthSSE(w http.ResponseWriter, r *http.Request) {
	// Replicate RequireAuth logic for net/http requests.
	loginRequired := true
	if cfg := s.configManager.GetConfig(); cfg != nil && cfg.Auth.LoginRequired != nil {
		loginRequired = *cfg.Auth.LoginRequired
	}
	if loginRequired && s.authService != nil {
		if ts := s.authService.TokenService(); ts != nil {
			if _, _, err := ts.Get(r); err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	subID, updateCh := s.progressBroadcaster.Subscribe()
	defer s.progressBroadcaster.Unsubscribe(subID)

	fmt.Fprintf(w, "data: {\"type\":\"initial\"}\n\n")
	flusher.Flush()

	keepAlive := time.NewTicker(30 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case update, ok := <-updateCh:
			if !ok {
				return
			}
			if update.Status != "health_changed" {
				continue
			}
			fmt.Fprintf(w, "data: {\"type\":\"update\"}\n\n")
			flusher.Flush()
		case <-keepAlive.C:
			fmt.Fprintf(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
