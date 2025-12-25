package api

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/javi11/altmount/internal/nzbfilesystem"
)

// Default timeout for stale streams (4 hours - covers most movie lengths)
const defaultStreamTimeout = 4 * time.Hour

// StreamTracker tracks active streams
type StreamTracker struct {
	streams sync.Map
	history []nzbfilesystem.ActiveStream
	done    chan struct{}
	mu      sync.Mutex // For history protection
	timeout time.Duration
}

type streamInternal struct {
	*nzbfilesystem.ActiveStream
	lastBytesSent int64
	lastSnapshot  time.Time
	lastReadAt    time.Time
	cancel        context.CancelFunc
}

// NewStreamTracker creates a new stream tracker
func NewStreamTracker() *StreamTracker {
	t := &StreamTracker{
		done:    make(chan struct{}),
		history: make([]nzbfilesystem.ActiveStream, 0, 50),
		timeout: defaultStreamTimeout,
	}
	go t.snapshotLoop()
	return t
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
		internal := value.(*streamInternal)
		stream := internal.ActiveStream
		if now.Sub(stream.StartedAt) > t.timeout {
			t.Remove(key.(string))
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

func (t *StreamTracker) Stop() {
	close(t.done)
}

func (t *StreamTracker) snapshotLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			t.streams.Range(func(key, value interface{}) bool {
				s := value.(*streamInternal)
				now := time.Now()

				// Cleanup stale streams (no activity for 30 minutes)
				// This handles cases where clients disconnect without properly closing the stream
				if !s.lastSnapshot.IsZero() && now.Sub(s.lastSnapshot) > 30*time.Minute {
					t.Remove(key.(string))
					return true
				}

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

				// Update Status
				if currentBytes == 0 {
					s.Status = "Buffering"
				} else if !s.lastReadAt.IsZero() && now.Sub(s.lastReadAt) > 10*time.Second {
					s.Status = "Stalled"
				} else {
					s.Status = "Streaming"
				}

				// Calculate Average Speed
				totalDuration := now.Sub(s.StartedAt).Seconds()
				if totalDuration > 0 {
					s.SpeedAvg = int64(float64(currentBytes) / totalDuration)
				}

				// Calculate ETA based on current speed
				if s.BytesPerSecond > 0 && s.TotalSize > 0 {
					currentOffset := atomic.LoadInt64(&s.CurrentOffset)
					// Use the greater of CurrentOffset or BytesSent to determine progress
					// This handles cases where offset tracking might be missing
					progress := currentOffset
					if currentBytes > progress {
						progress = currentBytes
					}
					
					remainingBytes := s.TotalSize - progress
					if remainingBytes > 0 {
						s.ETA = remainingBytes / s.BytesPerSecond
					} else {
						s.ETA = 0
					}
				} else {
					s.ETA = -1 // Unknown or Infinite
				}

				// Only update lastSnapshot if bytes were actually sent, otherwise it keeps the time of last activity
				if currentBytes > s.lastBytesSent || s.lastSnapshot.IsZero() {
					s.lastSnapshot = now
				}
				s.lastBytesSent = currentBytes
				return true
			})
		}
	}
}

// AddStream adds a new stream and returns the stream object for updates
func (t *StreamTracker) AddStream(filePath, source, userName, clientIP, userAgent string, totalSize int64) *nzbfilesystem.ActiveStream {
	id := uuid.New().String()
	now := time.Now()
	stream := &nzbfilesystem.ActiveStream{
		ID:           id,
		FilePath:     filePath,
		StartedAt:    now,
		LastActivity: now,
		Source:       source,
		UserName:     userName,
		ClientIP:     clientIP,
		UserAgent:    userAgent,
		TotalSize:    totalSize,
		Status:       "Starting",
	}
	internal := &streamInternal{
		ActiveStream: stream,
		lastSnapshot: now,
		lastReadAt:   now,
	}
	t.streams.Store(id, internal)
	return stream
}

// Add adds a new stream and returns its ID (implements nzbfilesystem.StreamTracker)
func (t *StreamTracker) Add(filePath, source, userName, clientIP, userAgent string, totalSize int64) string {
	return t.AddStream(filePath, source, userName, clientIP, userAgent, totalSize).ID
}

// SetCancelFunc sets the cancellation function for a stream
func (t *StreamTracker) SetCancelFunc(id string, cancel context.CancelFunc) {
	if val, ok := t.streams.Load(id); ok {
		internal := val.(*streamInternal)
		internal.cancel = cancel
	}
}

// UpdateProgress updates the bytes sent for a stream by ID
func (t *StreamTracker) UpdateProgress(id string, bytesRead int64) {
	if val, ok := t.streams.Load(id); ok {
		stream := val.(*streamInternal)
		atomic.AddInt64(&stream.BytesSent, bytesRead)
		stream.lastReadAt = time.Now()
	}
}

// UpdateBufferedOffset updates the buffered offset for a stream by ID
func (t *StreamTracker) UpdateBufferedOffset(id string, offset int64) {
	if val, ok := t.streams.Load(id); ok {
		stream := val.(*streamInternal)
		atomic.StoreInt64(&stream.BufferedOffset, offset)
	}
}

// Remove removes a stream by ID and adds it to history
func (t *StreamTracker) Remove(id string) {
	if val, ok := t.streams.Load(id); ok {
		internal := valueToInternal(val)

		// Cancel the context to stop underlying readers and release resources
		if internal.cancel != nil {
			internal.cancel()
		}

		// Capture final stats
		finalStream := *internal.ActiveStream
		finalStream.BytesSent = atomic.LoadInt64(&internal.BytesSent)
		finalStream.Status = "Completed"

		t.mu.Lock()
		// Keep last 50 streams in history
		if len(t.history) >= 50 {
			t.history = t.history[1:]
		}
		t.history = append(t.history, finalStream)
		t.mu.Unlock()

		t.streams.Delete(id)
	}
}

// KillStream cancels the context associated with a stream
func (t *StreamTracker) KillStream(id string) bool {
	if val, ok := t.streams.Load(id); ok {
		internal := val.(*streamInternal)
		if internal.cancel != nil {
			internal.cancel()
			return true
		}
	}
	return false
}

// GetHistory returns the recent stream history
func (t *StreamTracker) GetHistory() []nzbfilesystem.ActiveStream {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	// Return a copy of history, reversed (newest first)
	res := make([]nzbfilesystem.ActiveStream, len(t.history))
	for i, s := range t.history {
		res[len(t.history)-1-i] = s
	}
	return res
}

func valueToInternal(val interface{}) *streamInternal {
	return val.(*streamInternal)
}

// GetAll returns all active streams, aggregated by file, user, and source
func (t *StreamTracker) GetAll() []nzbfilesystem.ActiveStream {
	// Map to group streams: key -> *nzbfilesystem.ActiveStream
	grouped := make(map[string]*nzbfilesystem.ActiveStream)

	t.streams.Range(func(key, value interface{}) bool {
		internal := value.(*streamInternal)
		s := internal.ActiveStream

		// Create a composite key for grouping
		// We group by FilePath, UserName, Source, ClientIP and UserAgent to aggregate parallel connections
		// for the same playback session while keeping different devices separate
		groupKey := s.FilePath + "|" + s.UserName + "|" + s.Source + "|" + s.ClientIP + "|" + s.UserAgent

		if existing, ok := grouped[groupKey]; ok {
			// Aggregate with existing group
			
			// Sum up bytes sent from all connections
			currentBytes := atomic.LoadInt64(&s.BytesSent)
			existing.BytesSent += currentBytes
			existing.BytesPerSecond += internal.BytesPerSecond
			// Average speed is complex to aggregate, but sum of averages approximates total throughput
			existing.SpeedAvg += internal.SpeedAvg 

			// Use the current offset from the most recently active connection
			// This handles seek-back scenarios better than taking the max
			if internal.lastReadAt.After(existing.LastActivity) {
				existing.LastActivity = internal.lastReadAt
				existing.CurrentOffset = atomic.LoadInt64(&s.CurrentOffset)
				existing.BufferedOffset = atomic.LoadInt64(&s.BufferedOffset)
			}
			
			// For ETA, use the stream with the longest remaining time or re-calculate based on totals?
			// Re-calculating based on aggregated values is safer
			if existing.BytesPerSecond > 0 && existing.TotalSize > 0 {
				remaining := existing.TotalSize - existing.CurrentOffset
				
				if remaining > 0 {
					existing.ETA = remaining / existing.BytesPerSecond
				} else {
					existing.ETA = 0
				}
			}

			// Use the earliest start time to represent the session start
			if s.StartedAt.Before(existing.StartedAt) {
				existing.StartedAt = s.StartedAt
			}

			// Ensure we have the total size (should be consistent across connections)
			if existing.TotalSize == 0 && s.TotalSize > 0 {
				existing.TotalSize = s.TotalSize
			}

			// Use the "most active" status
			if existing.Status != "Streaming" && s.Status == "Streaming" {
				existing.Status = "Streaming"
			}

			existing.TotalConnections++
		} else {
			// Initialize new group with this stream
			streamCopy := *s
			// Load current atomic value
			streamCopy.BytesSent = atomic.LoadInt64(&s.BytesSent)
			streamCopy.CurrentOffset = atomic.LoadInt64(&s.CurrentOffset)
			streamCopy.BufferedOffset = atomic.LoadInt64(&s.BufferedOffset)
			streamCopy.LastActivity = internal.lastReadAt
			streamCopy.BytesPerSecond = internal.BytesPerSecond
			streamCopy.SpeedAvg = internal.SpeedAvg
			streamCopy.ETA = internal.ETA
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