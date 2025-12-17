package api

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

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
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewStreamTracker creates a new stream tracker
func NewStreamTracker() *StreamTracker {
	ctx, cancel := context.WithCancel(context.Background())
	st := &StreamTracker{
		ctx:    ctx,
		cancel: cancel,
	}
	
	// Start cleanup goroutine to remove stale streams
	go st.cleanupStaleStreams()
	
	return st
}

// Close stops the stream tracker and cleans up resources
func (st *StreamTracker) Close() {
	st.cancel()
	// Clear all streams
	st.streams.Range(func(key, value interface{}) bool {
		st.streams.Delete(key)
		return true
	})
}

// cleanupStaleStreams periodically removes streams that have been active for too long (likely abandoned)
func (st *StreamTracker) cleanupStaleStreams() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-st.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			st.streams.Range(func(key, value interface{}) bool {
				stream, ok := value.(ActiveStream)
				if !ok {
					st.streams.Delete(key)
					return true
				}
				// Remove streams that have been active for more than 1 hour (likely abandoned)
				if now.Sub(stream.StartedAt) > 1*time.Hour {
					st.streams.Delete(key)
				}
				return true
			})
		}
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
