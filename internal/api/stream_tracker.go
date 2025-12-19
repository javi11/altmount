package api

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Default timeout for stale streams (4 hours - covers most movie lengths)
const defaultStreamTimeout = 4 * time.Hour

// ActiveStream represents a file currently being streamed
type ActiveStream struct {
	ID        string    `json:"id"`
	FilePath  string    `json:"file_path"`
	ClientIP  string    `json:"client_ip"`
	StartedAt time.Time `json:"started_at"`
	UserAgent string    `json:"user_agent"`
	Range     string    `json:"range,omitempty"`
	Source    string    `json:"source"`
}

// StreamTracker tracks active streams
type StreamTracker struct {
	streams sync.Map
	timeout time.Duration
}

// NewStreamTracker creates a new stream tracker
func NewStreamTracker() *StreamTracker {
	return &StreamTracker{
		timeout: defaultStreamTimeout,
	}
}

// StartCleanup starts a background goroutine that periodically removes stale streams.
// Call this once during server startup. The cleanup runs every 5 minutes.
// The goroutine stops when the context is cancelled.
func (t *StreamTracker) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.cleanupStale()
			}
		}
	}()
}

// cleanupStale removes streams that have been active longer than the timeout.
// This handles cases where client disconnections don't properly trigger cleanup.
func (t *StreamTracker) cleanupStale() {
	now := time.Now()
	var removed int

	t.streams.Range(func(key, value interface{}) bool {
		stream := value.(ActiveStream)
		if now.Sub(stream.StartedAt) > t.timeout {
			t.streams.Delete(key)
			removed++
			slog.Debug("Cleaned up stale stream",
				"stream_id", stream.ID,
				"file_path", stream.FilePath,
				"started_at", stream.StartedAt,
				"age", now.Sub(stream.StartedAt))
		}
		return true
	})

	if removed > 0 {
		slog.Info("Cleaned up stale streams", "count", removed)
	}
}

// Add adds a new stream and returns its ID
func (t *StreamTracker) Add(filePath, clientIP, userAgent, rangeHeader, source string) string {
	id := uuid.New().String()
	stream := ActiveStream{
		ID:        id,
		FilePath:  filePath,
		ClientIP:  clientIP,
		StartedAt: time.Now(),
		UserAgent: userAgent,
		Range:     rangeHeader,
		Source:    source,
	}
	t.streams.Store(id, stream)
	return id
}

// Remove removes a stream by ID
func (t *StreamTracker) Remove(id string) {
	t.streams.Delete(id)
}

// GetAll returns all active streams
func (t *StreamTracker) GetAll() []ActiveStream {
	var streams []ActiveStream
	t.streams.Range(func(key, value interface{}) bool {
		streams = append(streams, value.(ActiveStream))
		return true
	})

	// Sort by start time, newest first
	sort.Slice(streams, func(i, j int) bool {
		return streams[i].StartedAt.After(streams[j].StartedAt)
	})

	return streams
}
