package api

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// ActiveStream represents a file currently being streamed
type ActiveStream struct {
	ID        string             `json:"id"`
	FilePath  string             `json:"file_path"`
	StartedAt time.Time          `json:"started_at"`
	Source    string             `json:"source"`
	UserName  string             `json:"user_name,omitempty"`
	TotalSize int64              `json:"total_size"`
	BytesSent int64              `json:"bytes_sent"`
	cancel    context.CancelFunc `json:"-"`
}

// StreamTracker tracks active streams
type StreamTracker struct {
	streams sync.Map
}

// NewStreamTracker creates a new stream tracker
func NewStreamTracker() *StreamTracker {
	return &StreamTracker{}
}

// Add adds a new stream and returns the stream object for updates
func (t *StreamTracker) Add(filePath, source, userName string, totalSize int64, cancel context.CancelFunc) *ActiveStream {
	id := uuid.New().String()
	stream := &ActiveStream{
		ID:        id,
		FilePath:  filePath,
		StartedAt: time.Now(),
		Source:    source,
		UserName:  userName,
		TotalSize: totalSize,
		cancel:    cancel,
	}
	t.streams.Store(id, stream)
	return stream
}

// Remove removes a stream by ID
func (t *StreamTracker) Remove(id string) {
	t.streams.Delete(id)
}

// Stop terminates a stream by ID
func (t *StreamTracker) Stop(id string) bool {
	if val, ok := t.streams.Load(id); ok {
		stream := val.(*ActiveStream)
		if stream.cancel != nil {
			stream.cancel()
			return true
		}
	}
	return false
}

// GetAll returns all active streams, aggregated by file, user, and source
func (t *StreamTracker) GetAll() []ActiveStream {
	// Map to group streams: key -> *ActiveStream
	grouped := make(map[string]*ActiveStream)

	t.streams.Range(func(key, value interface{}) bool {
		s := value.(*ActiveStream)

		// Create a composite key for grouping
		// We group by FilePath, UserName and Source to aggregate parallel connections
		// for the same playback session
		groupKey := s.FilePath + "|" + s.UserName + "|" + s.Source

		if existing, ok := grouped[groupKey]; ok {
			// Aggregate with existing group
			
			// Sum up bytes sent from all connections
			currentBytes := atomic.LoadInt64(&s.BytesSent)
			existing.BytesSent += currentBytes

			// Use the earliest start time to represent the session start
			if s.StartedAt.Before(existing.StartedAt) {
				existing.StartedAt = s.StartedAt
			}

			// Ensure we have the total size (should be consistent across connections)
			if existing.TotalSize == 0 && s.TotalSize > 0 {
				existing.TotalSize = s.TotalSize
			}
		} else {
			// Initialize new group with this stream
			streamCopy := *s
			// Load current atomic value
			streamCopy.BytesSent = atomic.LoadInt64(&s.BytesSent)
			grouped[groupKey] = &streamCopy
		}
		return true
	})

	// Convert map to slice
	var streams []ActiveStream
	for _, s := range grouped {
		streams = append(streams, *s)
	}

	// Sort by start time, newest first
	sort.Slice(streams, func(i, j int) bool {
		return streams[i].StartedAt.After(streams[j].StartedAt)
	})

	return streams
}
