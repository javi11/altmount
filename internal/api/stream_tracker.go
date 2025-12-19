package api

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/javi11/altmount/internal/nzbfilesystem"
)

// StreamTracker tracks active streams
type StreamTracker struct {
	streams sync.Map
}

type streamInternal struct {
	*nzbfilesystem.ActiveStream
	lastBytesSent int64
	lastSnapshot  time.Time
}

// NewStreamTracker creates a new stream tracker
func NewStreamTracker() *StreamTracker {
	t := &StreamTracker{}
	go t.snapshotLoop()
	return t
}

func (t *StreamTracker) snapshotLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		t.streams.Range(func(key, value interface{}) bool {
			s := value.(*streamInternal)
			now := time.Now()
			currentBytes := atomic.LoadInt64(&s.BytesSent)

			if !s.lastSnapshot.IsZero() {
				duration := now.Sub(s.lastSnapshot).Seconds()
				if duration > 0 {
					bytesDiff := currentBytes - s.lastBytesSent
					if bytesDiff < 0 {
						bytesDiff = 0
					}
					s.BytesPerSecond = int64(float64(bytesDiff) / duration)
				}
			}

			s.lastBytesSent = currentBytes
			s.lastSnapshot = now
			return true
		})
	}
}

// AddStream adds a new stream and returns the stream object for updates
func (t *StreamTracker) AddStream(filePath, source, userName string, totalSize int64) *nzbfilesystem.ActiveStream {
	id := uuid.New().String()
	stream := &nzbfilesystem.ActiveStream{
		ID:        id,
		FilePath:  filePath,
		StartedAt: time.Now(),
		Source:    source,
		UserName:  userName,
		TotalSize: totalSize,
	}
	internal := &streamInternal{
		ActiveStream: stream,
		lastSnapshot: time.Now(),
	}
	t.streams.Store(id, internal)
	return stream
}

// Add adds a new stream and returns its ID (implements nzbfilesystem.StreamTracker)
func (t *StreamTracker) Add(filePath, source, userName string, totalSize int64) string {
	return t.AddStream(filePath, source, userName, totalSize).ID
}

// UpdateProgress updates the bytes sent for a stream by ID
func (t *StreamTracker) UpdateProgress(id string, bytesRead int64) {
	if val, ok := t.streams.Load(id); ok {
		stream := val.(*streamInternal)
		atomic.AddInt64(&stream.BytesSent, bytesRead)
	}
}

// Remove removes a stream by ID
func (t *StreamTracker) Remove(id string) {
	t.streams.Delete(id)
}

// GetAll returns all active streams, aggregated by file, user, and source
func (t *StreamTracker) GetAll() []nzbfilesystem.ActiveStream {
	// Map to group streams: key -> *nzbfilesystem.ActiveStream
	grouped := make(map[string]*nzbfilesystem.ActiveStream)

	t.streams.Range(func(key, value interface{}) bool {
		internal := value.(*streamInternal)
		s := internal.ActiveStream

		// Create a composite key for grouping
		// We group by FilePath, UserName and Source to aggregate parallel connections
		// for the same playback session
		groupKey := s.FilePath + "|" + s.UserName + "|" + s.Source

		if existing, ok := grouped[groupKey]; ok {
			// Aggregate with existing group
			
			// Sum up bytes sent from all connections
			currentBytes := atomic.LoadInt64(&s.BytesSent)
			existing.BytesSent += currentBytes
			existing.BytesPerSecond += internal.BytesPerSecond

			// Use the earliest start time to represent the session start
			if s.StartedAt.Before(existing.StartedAt) {
				existing.StartedAt = s.StartedAt
			}

			// Ensure we have the total size (should be consistent across connections)
			if existing.TotalSize == 0 && s.TotalSize > 0 {
				existing.TotalSize = s.TotalSize
			}

			existing.TotalConnections++
		} else {
			// Initialize new group with this stream
			streamCopy := *s
			// Load current atomic value
			streamCopy.BytesSent = atomic.LoadInt64(&s.BytesSent)
			streamCopy.BytesPerSecond = internal.BytesPerSecond
			// Use groupKey as stable ID to prevent UI flickering when underlying connections change
			streamCopy.ID = groupKey
			streamCopy.TotalConnections = 1
			grouped[groupKey] = &streamCopy
		}
		return true
	})

	// Convert map to slice
	var streams []nzbfilesystem.ActiveStream
	for _, s := range grouped {
		streams = append(streams, *s)
	}

	// Sort by start time, newest first
	sort.Slice(streams, func(i, j int) bool {
		return streams[i].StartedAt.After(streams[j].StartedAt)
	})

	return streams
}

// GetStream returns an active stream by ID
func (t *StreamTracker) GetStream(id string) *nzbfilesystem.ActiveStream {
	if val, ok := t.streams.Load(id); ok {
		return val.(*streamInternal).ActiveStream
	}
	return nil
}
