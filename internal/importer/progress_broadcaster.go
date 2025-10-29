package importer

import (
	"log/slog"
	"sync"
)

// ProgressBroadcaster manages progress tracking for queue items
type ProgressBroadcaster struct {
	// Map of queue item ID to current progress percentage
	progress map[int]int
	// Mutex for thread-safe access
	mu sync.RWMutex
	// Logger
	log *slog.Logger
}

// NewProgressBroadcaster creates a new progress broadcaster
func NewProgressBroadcaster() *ProgressBroadcaster {
	pb := &ProgressBroadcaster{
		progress: make(map[int]int),
		log:      slog.Default().With("component", "progress-broadcaster"),
	}

	return pb
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

	pb.log.Debug("progress updated",
		"queue_id", queueID,
		"percentage", percentage)
}

// ClearProgress removes progress tracking for a completed or failed queue item
func (pb *ProgressBroadcaster) ClearProgress(queueID int) {
	pb.mu.Lock()
	delete(pb.progress, queueID)
	pb.mu.Unlock()

	pb.log.Debug("progress cleared", "queue_id", queueID)
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
