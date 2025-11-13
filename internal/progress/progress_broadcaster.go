package progress

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ProgressUpdate represents a progress update event
type ProgressUpdate struct {
	QueueID    int       `json:"queue_id"`
	Percentage int       `json:"percentage"`
	Timestamp  time.Time `json:"timestamp"`
}

// ProgressBroadcaster manages progress tracking for queue items
type ProgressBroadcaster struct {
	// Map of queue item ID to current progress percentage
	progress map[int]int
	// Mutex for thread-safe access
	mu sync.RWMutex
	// Logger
	log *slog.Logger
	// SSE subscribers
	subscribers map[string]chan ProgressUpdate
	subMu       sync.RWMutex
}

// NewProgressBroadcaster creates a new progress broadcaster
func NewProgressBroadcaster() *ProgressBroadcaster {
	pb := &ProgressBroadcaster{
		progress:    make(map[int]int),
		subscribers: make(map[string]chan ProgressUpdate),
		log:         slog.Default().With("component", "progress-broadcaster"),
	}

	return pb
}

func (pb *ProgressBroadcaster) Close() error {
	pb.subMu.Lock()
	defer pb.subMu.Unlock()
	for _, ch := range pb.subscribers {
		close(ch)
	}
	pb.subscribers = make(map[string]chan ProgressUpdate)

	return nil
}

// UpdateProgress updates the progress for a queue item
func (pb *ProgressBroadcaster) UpdateProgress(queueID int, percentage int) {
	// Clamp percentage to 0-100 range
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}

	pb.mu.Lock()
	if percentage >= 100 {
		// Remove progress when complete (100%)
		delete(pb.progress, queueID)
	} else {
		pb.progress[queueID] = percentage
	}
	pb.mu.Unlock()

	// Broadcast update to all SSE subscribers
	update := ProgressUpdate{
		QueueID:    queueID,
		Percentage: percentage,
		Timestamp:  time.Now(),
	}

	pb.subMu.RLock()
	for subID, ch := range pb.subscribers {
		select {
		case ch <- update:
			// Successfully sent update
		default:
			// Channel full, skip this subscriber to avoid blocking
			pb.log.WarnContext(context.Background(), "subscriber channel full, skipping update", "subscriber_id", subID, "queue_id", queueID)
		}
	}
	pb.subMu.RUnlock()
}

// ClearProgress removes progress tracking for a completed or failed queue item
func (pb *ProgressBroadcaster) ClearProgress(queueID int) {
	pb.mu.Lock()
	delete(pb.progress, queueID)
	pb.mu.Unlock()
}

// GetProgress returns the current progress for a queue item
func (pb *ProgressBroadcaster) GetProgress(queueID int) (int, bool) {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	percentage, exists := pb.progress[queueID]
	return percentage, exists
}

// GetAllProgress returns a copy of all current progress states
func (pb *ProgressBroadcaster) GetAllProgress() map[int]int {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	progressCopy := make(map[int]int, len(pb.progress))
	for id, percentage := range pb.progress {
		progressCopy[id] = percentage
	}
	return progressCopy
}

// CreateTracker creates a progress tracker for a specific queue item with a percentage range
func (pb *ProgressBroadcaster) CreateTracker(queueID, minPercent, maxPercent int) *Tracker {
	return NewTracker(pb, queueID, minPercent, maxPercent)
}

// Subscribe creates a new SSE subscriber and returns a subscription ID and update channel
func (pb *ProgressBroadcaster) Subscribe() (string, <-chan ProgressUpdate) {
	pb.subMu.Lock()
	defer pb.subMu.Unlock()

	// Generate unique subscriber ID
	subID := fmt.Sprintf("sub-%d", time.Now().UnixNano())

	// Create buffered channel to prevent slow consumers from blocking
	ch := make(chan ProgressUpdate, 10)
	pb.subscribers[subID] = ch

	return subID, ch
}

// Unsubscribe removes an SSE subscriber and closes its channel
func (pb *ProgressBroadcaster) Unsubscribe(subID string) {
	pb.subMu.Lock()
	defer pb.subMu.Unlock()

	if ch, exists := pb.subscribers[subID]; exists {
		close(ch)
		delete(pb.subscribers, subID)
	}
}
