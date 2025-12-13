package api

import (
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
}

// NewStreamTracker creates a new stream tracker
func NewStreamTracker() *StreamTracker {
	return &StreamTracker{}
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
