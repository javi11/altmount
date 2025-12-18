package api

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// ActiveStream represents a file currently being streamed
type ActiveStream struct {
	ID        string    `json:"id"`
	FilePath  string    `json:"file_path"`
	StartedAt time.Time `json:"started_at"`
	Source    string    `json:"source"`
	UserName  string    `json:"user_name,omitempty"`
	TotalSize int64     `json:"total_size"`
	BytesSent int64     `json:"bytes_sent"`
}

// StreamTracker tracks active streams
type StreamTracker struct {
	streams sync.Map
}

// NewStreamTracker creates a new stream tracker
func NewStreamTracker() *StreamTracker {
	return &StreamTracker{}
}

// AddStream adds a new stream and returns the stream object for updates
func (t *StreamTracker) AddStream(filePath, source, userName string, totalSize int64) *ActiveStream {
	id := uuid.New().String()
	stream := &ActiveStream{
		ID:        id,
		FilePath:  filePath,
		StartedAt: time.Now(),
		Source:    source,
		UserName:  userName,
		TotalSize: totalSize,
	}
	t.streams.Store(id, stream)
	return stream
}

// Add adds a new stream and returns its ID (implements nzbfilesystem.StreamTracker)
func (t *StreamTracker) Add(filePath, source, userName string, totalSize int64) string {
	return t.AddStream(filePath, source, userName, totalSize).ID
}

// Remove removes a stream by ID
func (t *StreamTracker) Remove(id string) {
	t.streams.Delete(id)
}

// GetAll returns all active streams
func (t *StreamTracker) GetAll() []ActiveStream {
	var streams []ActiveStream
	t.streams.Range(func(key, value interface{}) bool {
		s := value.(*ActiveStream)
		// Create a copy to avoid race conditions and ensure atomic read
		streamCopy := *s
		streamCopy.BytesSent = atomic.LoadInt64(&s.BytesSent)
		streams = append(streams, streamCopy)
		return true
	})

	// Sort by start time, newest first
	sort.Slice(streams, func(i, j int) bool {
		return streams[i].StartedAt.After(streams[j].StartedAt)
	})

	return streams
}
